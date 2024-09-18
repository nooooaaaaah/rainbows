package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/charmbracelet/log"
)

const baseURL = "https://api.weather.gov"

type PointMetadata struct {
	Properties struct {
		GridId         string `json:"gridId"`
		GridX          int    `json:"gridX"`
		GridY          int    `json:"gridY"`
		ForecastHref   string `json:"forecast"`
		ForecastHourly string `json:"forecastHourly"`
		ForecastZone   string `json:"forecastZone"`
	} `json:"properties"`
}

type ForecastData struct {
	Properties struct {
		Periods []struct {
			StartTime                  string  `json:"startTime"`
			Temperature                float64 `json:"temperature"`
			ProbabilityOfPrecipitation struct {
				Value float64 `json:"value"`
			} `json:"probabilityOfPrecipitation"`
			ShortForecast string `json:"shortForecast"`
			WindSpeed     string `json:"windSpeed"`
			WindDirection string `json:"windDirection"`
			CloudCover    int    `json:"cloudCover"`
		} `json:"periods"`
	} `json:"properties"`
}

type RainbowPrediction struct {
	Likelihood float64   `json:"likelihood"`
	Location   string    `json:"location"`
	Time       time.Time `json:"time"`
}

type CacheEntry struct {
	Data      interface{}
	ExpiresAt time.Time
}

var (
	logger *log.Logger
	client *http.Client
	cache  = make(map[string]CacheEntry)
	mutex  sync.RWMutex
)

func init() {
	logger = log.NewWithOptions(os.Stdout, log.Options{
		Prefix: "RAINBOW_APP",
		Level:  log.DebugLevel,
	})
	client = &http.Client{Timeout: 10 * time.Second}
}

func fetchJSONWithRetry(url string, target interface{}, maxRetries int) error {
	var err error
	for i := 0; i < maxRetries; i++ {
		err = fetchJSON(url, target)
		if err == nil {
			return nil
		}
		logger.Error("Error fetching JSON", "retry", i+1, "url", url, "error", err)
		time.Sleep(time.Duration(i+1) * time.Second)
	}
	return fmt.Errorf("max retries reached: %w", err)
}

func fetchJSON(url string, target interface{}) error {
	mutex.RLock()
	if entry, ok := cache[url]; ok && time.Now().Before(entry.ExpiresAt) {
		mutex.RUnlock()
		*target.(*interface{}) = entry.Data
		return nil
	}
	mutex.RUnlock()

	logger.Info("Fetching JSON", "url", url)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("User-Agent", "RainbowPredictionApp/1.0 (your@email.com)")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error fetching JSON: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %w", err)
	}

	err = json.Unmarshal(body, target)
	if err != nil {
		return fmt.Errorf("error unmarshalling JSON: %w", err)
	}

	mutex.Lock()
	cache[url] = CacheEntry{
		Data:      target,
		ExpiresAt: time.Now().Add(15 * time.Minute),
	}
	mutex.Unlock()

	return nil
}

func fetchPointMetadata(lat, lon float64) (PointMetadata, error) {
	var metadata PointMetadata
	url := fmt.Sprintf("%s/points/%.4f,%.4f", baseURL, lat, lon)

	err := fetchJSONWithRetry(url, &metadata, 3)
	if err != nil {
		return PointMetadata{}, fmt.Errorf("error fetching point metadata: %w", err)
	}

	return metadata, nil
}

func fetchForecast(metadata PointMetadata) (ForecastData, error) {
	var forecast ForecastData
	forecastURL := metadata.Properties.ForecastHourly
	if forecastURL == "" {
		return ForecastData{}, fmt.Errorf("hourly forecast URL not available")
	}

	err := fetchJSONWithRetry(forecastURL, &forecast, 3)
	if err != nil {
		return ForecastData{}, fmt.Errorf("error fetching hourly forecast: %w", err)
	}

	return forecast, nil
}

func calculateSunAngle(t time.Time, lat, lon float64) float64 {
	// This is a simplified calculation and should be replaced with a more accurate method
	hour := float64(t.Hour()) + float64(t.Minute())/60.0
	return 90 - math.Abs(hour-12)*15
}

func predictRainbow(forecast ForecastData, lat, lon float64) RainbowPrediction {
	var bestLikelihood float64
	var bestTime time.Time

	for _, period := range forecast.Properties.Periods {
		startTime, err := time.Parse(time.RFC3339, period.StartTime)
		if err != nil {
			logger.Error("Error parsing start time", "startTime", period.StartTime, "error", err)
			continue
		}

		sunAngle := calculateSunAngle(startTime, lat, lon)
		if sunAngle < 0 || sunAngle > 42 {
			continue
		}

		if period.CloudCover > 96 {
			continue
		}

		// Check for presence of liquid precipitation
		hasPrecipitation := period.ProbabilityOfPrecipitation.Value > 0 && period.Temperature > 32

		if !hasPrecipitation {
			continue
		}

		likelihood := 1.0
		likelihood *= (100 - float64(period.CloudCover)) / 100
		likelihood *= period.ProbabilityOfPrecipitation.Value / 100

		// Adjust for optimal sun angle
		angleFactor := 1 - math.Abs(sunAngle-21)/21
		likelihood *= angleFactor

		// Give higher weight to predictions between 16:00 and 22:00 local time
		hour := startTime.Hour()
		if hour >= 16 && hour <= 22 {
			likelihood *= 1.5
		}

		// Seasonal adjustment (simple example - can be refined)
		month := startTime.Month()
		if month >= time.April && month <= time.September {
			likelihood *= 1.2
		}

		if likelihood > bestLikelihood {
			bestLikelihood = likelihood
			bestTime = startTime
		}
	}

	return RainbowPrediction{
		Likelihood: bestLikelihood,
		Location:   fmt.Sprintf("%.4f, %.4f", lat, lon),
		Time:       bestTime,
	}
}

func handlePrediction(w http.ResponseWriter, r *http.Request) {
	lat, err := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	if err != nil {
		http.Error(w, "Invalid latitude", http.StatusBadRequest)
		return
	}
	lon, err := strconv.ParseFloat(r.URL.Query().Get("lon"), 64)
	if err != nil {
		http.Error(w, "Invalid longitude", http.StatusBadRequest)
		return
	}

	// Check if the coordinates are within Colorado
	if lat < 37 || lat > 41 || lon < -109 || lon > -102 {
		http.Error(w, "Coordinates are outside of Colorado", http.StatusBadRequest)
		return
	}

	metadata, err := fetchPointMetadata(lat, lon)
	if err != nil {
		logger.Error("Error fetching point metadata", "error", err)
		http.Error(w, "Error fetching weather data", http.StatusInternalServerError)
		return
	}

	forecast, err := fetchForecast(metadata)
	if err != nil {
		logger.Error("Error fetching forecast", "error", err)
		http.Error(w, "Error fetching weather data", http.StatusInternalServerError)
		return
	}

	prediction := predictRainbow(forecast, lat, lon)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(prediction)
}

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tmpl := template.Must(template.ParseFiles("index.html"))
		w.Header().Set("Content-Type", "text/html")
		tmpl.Execute(w, nil)
	})
	http.HandleFunc("/predict", handlePrediction)

	port := 8080
	logger.Info("Server starting", "port", port)
	err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
	if err != nil {
		logger.Fatal("Error starting server", "error", err)
	}
}
