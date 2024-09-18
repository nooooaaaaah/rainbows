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
	"strings"
	"time"

	"github.com/charmbracelet/log"
)

const baseURL = "https://api.weather.gov"

type PointMetadata struct {
	Properties struct {
		GridId       string `json:"gridId"`
		GridX        int    `json:"gridX"`
		GridY        int    `json:"gridY"`
		ForecastHref string `json:"forecast"`
		Forecast     string `json:"forecastHourly"`
		ForecastZone string `json:"forecastZone"`
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

var (
	logger *log.Logger
	client *http.Client
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

	return nil
}

func fetchPointMetadata(lat, lon float64) (PointMetadata, error) {
	var metadata PointMetadata
	url := fmt.Sprintf("%s/points/%.4f,%.4f", baseURL, lat, lon)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return PointMetadata{}, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("User-Agent", "RainbowPredictionApp/1.0 (your@email.com)")

	resp, err := client.Do(req)
	if err != nil {
		return PointMetadata{}, fmt.Errorf("error fetching point metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return PointMetadata{}, fmt.Errorf("no data available for coordinates: %.4f, %.4f", lat, lon)
	}

	if resp.StatusCode != http.StatusOK {
		return PointMetadata{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	err = json.NewDecoder(resp.Body).Decode(&metadata)
	if err != nil {
		return PointMetadata{}, fmt.Errorf("error decoding JSON: %w", err)
	}

	return metadata, nil
}

func fetchForecast(metadata PointMetadata) (ForecastData, error) {
	var forecast ForecastData
	hourlyForecastURL := metadata.Properties.Forecast
	if hourlyForecastURL == "" {
		return ForecastData{}, fmt.Errorf("forecast URL not available")
	}
	hourlyForecastURL = strings.Replace(hourlyForecastURL, "forecast", "forecast/hourly", 1)
	err := fetchJSONWithRetry(hourlyForecastURL, &forecast, 3)
	if err != nil {
		return ForecastData{}, fmt.Errorf("error fetching hourly forecast: %w", err)
	}
	return forecast, nil
}

func predictRainbow(forecast ForecastData, location string) RainbowPrediction {
	var bestLikelihood float64
	var bestTime time.Time

	for _, period := range forecast.Properties.Periods {
		startTime, err := time.Parse(time.RFC3339, period.StartTime)
		if err != nil {
			logger.Error("Error parsing start time", "startTime", period.StartTime, "error", err)
			continue
		}

		hour := startTime.Hour()
		if hour < 6 || hour > 20 {
			continue
		}

		precipProb := period.ProbabilityOfPrecipitation.Value
		likelihood := precipProb * (100 - precipProb) / 2500

		if period.Temperature <= 32 {
			likelihood = 0
		}

		cloudCoverFactor := 1 - math.Abs(float64(period.CloudCover)-50)/50
		likelihood *= cloudCoverFactor

		windSpeed := strings.Split(period.WindSpeed, " ")[0]
		windSpeedValue, err := strconv.ParseFloat(windSpeed, 64)
		if err != nil {
			logger.Error("Error parsing wind speed", "windSpeed", windSpeed, "error", err)
			windSpeedValue = 0
		}
		windFactor := 1 - math.Abs(windSpeedValue-10)/10
		if windFactor < 0 {
			windFactor = 0
		}
		likelihood *= windFactor

		if (strings.Contains(strings.ToLower(period.ShortForecast), "rain") || strings.Contains(strings.ToLower(period.ShortForecast), "showers")) &&
			(period.WindDirection == "E" || period.WindDirection == "NE" || period.WindDirection == "SE") {
			likelihood *= 1.5
		}

		sunAngle := calculateSunAngle(startTime, location)
		if sunAngle >= 0 && sunAngle <= 42 {
			likelihood *= 1.5
		}

		if likelihood > bestLikelihood {
			bestLikelihood = likelihood
			bestTime = startTime
		}
	}

	return RainbowPrediction{
		Likelihood: bestLikelihood,
		Location:   location,
		Time:       bestTime,
	}
}

func calculateSunAngle(t time.Time, location string) float64 {
	// This is a simplified calculation and should be replaced with a more accurate method
	hour := float64(t.Hour()) + float64(t.Minute())/60.0
	return 90 - math.Abs(hour-12)*15
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

	location := fmt.Sprintf("%.4f, %.4f", lat, lon)
	prediction := predictRainbow(forecast, location)

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
