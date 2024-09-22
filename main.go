package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gorilla/mux"
)

// baseURL is the endpoint for the OpenWeatherMap API
const (
	baseURL = "https://api.openweathermap.org/data/3.0/onecall"
)

// apiKey is the authentication token for the OpenWeatherMap API
var apiKey = "7d4c9a66d83ea191504f10e3e96afb23"

// WeatherCondition represents a specific weather condition with its ID and description
type WeatherCondition struct {
	ID          int    `json:"id"`
	Description string `json:"description"`
}

// WeatherData represents the structure of the weather data received from the API
type WeatherData struct {
	Current struct {
		Dt         int64              `json:"dt"`
		Temp       float64            `json:"temp"`
		Humidity   int                `json:"humidity"`
		Weather    []WeatherCondition `json:"weather"`
		Clouds     int                `json:"clouds"`
		UVI        float64            `json:"uvi"`
		Visibility int                `json:"visibility"`
		WindSpeed  float64            `json:"wind_speed"`
		WindDeg    int                `json:"wind_deg"`
	} `json:"current"`
	Hourly []struct {
		Dt         int64              `json:"dt"`
		Temp       float64            `json:"temp"`
		Humidity   int                `json:"humidity"`
		Weather    []WeatherCondition `json:"weather"`
		Clouds     int                `json:"clouds"`
		UVI        float64            `json:"uvi"`
		Visibility int                `json:"visibility"`
		WindSpeed  float64            `json:"wind_speed"`
		WindDeg    int                `json:"wind_deg"`
		Pop        float64            `json:"pop"`
	} `json:"hourly"`
}

// RainbowPrediction represents the prediction result for rainbow occurrence
type RainbowPrediction struct {
	Likelihood float64 `json:"likelihood"`
	Location   string  `json:"location"`
	Time       string  `json:"time"`
}

// HeatmapData represents the structure of the heatmap data
type HeatmapData struct {
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	Likelihood float64 `json:"likelihood"`
}

// fetchWeatherData retrieves weather data from the OpenWeatherMap API for given coordinates
func fetchWeatherData(lat, lon float64) (WeatherData, error) {
	url := fmt.Sprintf("%s?lat=%f&lon=%f&exclude=hourly,daily&units=metric&appid=%s", baseURL, lat, lon, apiKey)
	log.Debug("Fetching weather data", "url", url)
	resp, err := http.Get(url)
	if err != nil {
		log.Error("Error making request", "error", err)
		return WeatherData{}, fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Error("API request failed", "status_code", resp.StatusCode)
		return WeatherData{}, fmt.Errorf("API request failed with status code: %d", resp.StatusCode)
	}

	var weatherData WeatherData
	if err := json.NewDecoder(resp.Body).Decode(&weatherData); err != nil {
		log.Error("Error decoding response", "error", err)
		return WeatherData{}, fmt.Errorf("error decoding response: %w", err)
	}

	log.Debug("Weather data fetched successfully", "data", weatherData)
	return weatherData, nil
}

// calculateRainbowLikelihood computes the likelihood of a rainbow occurrence based on weather conditions
func calculateRainbowLikelihood(weather struct {
	Temp       float64
	Humidity   int
	Weather    []WeatherCondition
	Clouds     int
	UVI        float64
	Visibility int
	WindSpeed  float64
	WindDeg    int
	Pop        float64
}) float64 {
	log.Debug("Calculating rainbow likelihood", "weather_data", weather)
	// Check if weather conditions are suitable for rainbow formation
	if len(weather.Weather) == 0 || weather.Weather[0].ID < 200 || weather.Weather[0].ID >= 700 {
		log.Debug("Weather conditions not suitable for rainbow", "weather_id", weather.Weather[0].ID)
		return 0
	}

	// Calculate factors affecting rainbow likelihood
	cloudFactor := 1 - float64(weather.Clouds)/100
	humidityFactor := float64(weather.Humidity) / 100
	uviFactor := math.Min(weather.UVI/10, 1)                           // Normalize UVI to 0-1 range
	visibilityFactor := math.Min(float64(weather.Visibility)/10000, 1) // Normalize visibility to 0-1 range
	windFactor := 1 - math.Min(weather.WindSpeed/20, 1)                // Inverse wind speed factor

	likelihood := (cloudFactor + humidityFactor + uviFactor + visibilityFactor + windFactor) / 5

	// Increase likelihood if there's rain or high probability of precipitation
	if weather.Weather[0].ID >= 300 && weather.Weather[0].ID < 600 {
		log.Debug("Increased likelihood due to rain", "weather_id", weather.Weather[0].ID)
		likelihood *= 1.5
	} else if weather.Pop > 0.5 {
		log.Debug("Increased likelihood due to high precipitation probability", "pop", weather.Pop)
		likelihood *= 1.3
	}

	// Ensure likelihood is not greater than 1
	finalLikelihood := math.Min(likelihood, 1.0)
	log.Info("Rainbow likelihood calculated", "likelihood", finalLikelihood)
	return finalLikelihood
}

// handlePrediction processes the prediction request and returns the rainbow prediction
func handlePrediction(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	lat, err := strconv.ParseFloat(vars["lat"], 64)
	if err != nil {
		log.Error("Invalid latitude", "error", err)
		http.Error(w, "Invalid latitude", http.StatusBadRequest)
		return
	}
	lon, err := strconv.ParseFloat(vars["lon"], 64)
	if err != nil {
		log.Error("Invalid longitude", "error", err)
		http.Error(w, "Invalid longitude", http.StatusBadRequest)
		return
	}

	log.Info("Handling prediction request", "latitude", lat, "longitude", lon)

	weatherData, err := fetchWeatherData(lat, lon)
	if err != nil {
		log.Error("Error fetching weather data", "error", err)
		http.Error(w, fmt.Sprintf("Error fetching weather data: %v", err), http.StatusInternalServerError)
		return
	}

	var bestLikelihood float64
	var bestTime time.Time

	// Find the time with the highest rainbow likelihood
	for _, hourly := range weatherData.Hourly {
		likelihood := calculateRainbowLikelihood(struct {
			Temp       float64
			Humidity   int
			Weather    []WeatherCondition
			Clouds     int
			UVI        float64
			Visibility int
			WindSpeed  float64
			WindDeg    int
			Pop        float64
		}{
			Temp:       hourly.Temp,
			Humidity:   hourly.Humidity,
			Weather:    hourly.Weather,
			Clouds:     hourly.Clouds,
			UVI:        hourly.UVI,
			Visibility: hourly.Visibility,
			WindSpeed:  hourly.WindSpeed,
			WindDeg:    hourly.WindDeg,
			Pop:        hourly.Pop,
		})

		if likelihood > bestLikelihood {
			bestLikelihood = likelihood
			bestTime = time.Unix(hourly.Dt, 0)
		}
	}

	// Create the prediction result
	prediction := RainbowPrediction{
		Likelihood: bestLikelihood,
		Location:   fmt.Sprintf("%.4f, %.4f", lat, lon),
		Time:       bestTime.Format(time.RFC3339),
	}

	log.Info("Prediction calculated", "prediction", prediction)

	// Send the prediction as JSON response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(prediction)
}

// handleHeatmapData processes the heatmap data request
func handleHeatmapData(w http.ResponseWriter, r *http.Request) {
	lat, err := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	if err != nil {
		log.Error("Invalid latitude", "error", err)
		http.Error(w, "Invalid latitude", http.StatusBadRequest)
		return
	}
	lon, err := strconv.ParseFloat(r.URL.Query().Get("lon"), 64)
	if err != nil {
		log.Error("Invalid longitude", "error", err)
		http.Error(w, "Invalid longitude", http.StatusBadRequest)
		return
	}
	radius, err := strconv.ParseFloat(r.URL.Query().Get("radius"), 64)
	if err != nil {
		log.Error("Invalid radius", "error", err)
		http.Error(w, "Invalid radius", http.StatusBadRequest)
		return
	}
	resolution, err := strconv.ParseFloat(r.URL.Query().Get("resolution"), 64)
	if err != nil {
		resolution = 0.05 // Default resolution if not provided or invalid
	}

	log.Info("Handling heatmap data request", "lat", lat, "lon", lon, "radius", radius, "resolution", resolution)

	var heatmapData []HeatmapData

	// Convert radius from miles to degrees (approximate)
	radiusDegrees := radius / 69 // 1 degree is approximately 69 miles

	for dlat := -radiusDegrees; dlat <= radiusDegrees; dlat += resolution {
		for dlon := -radiusDegrees; dlon <= radiusDegrees; dlon += resolution {
			pointLat := lat + dlat
			pointLon := lon + dlon

			// Check if the point is within the radius
			if math.Sqrt(dlat*dlat+dlon*dlon) <= radiusDegrees {
				weatherData, err := fetchWeatherData(pointLat, pointLon)
				if err != nil {
					log.Error("Error fetching weather data", "error", err, "lat", pointLat, "lon", pointLon)
					continue
				}

				likelihood := calculateRainbowLikelihood(struct {
					Temp       float64
					Humidity   int
					Weather    []WeatherCondition
					Clouds     int
					UVI        float64
					Visibility int
					WindSpeed  float64
					WindDeg    int
					Pop        float64
				}{
					Temp:       weatherData.Current.Temp,
					Humidity:   weatherData.Current.Humidity,
					Weather:    weatherData.Current.Weather,
					Clouds:     weatherData.Current.Clouds,
					UVI:        weatherData.Current.UVI,
					Visibility: weatherData.Current.Visibility,
					WindSpeed:  weatherData.Current.WindSpeed,
					WindDeg:    weatherData.Current.WindDeg,
					Pop:        0, // Current data doesn't have Pop, so we set it to 0
				})
				heatmapData = append(heatmapData, HeatmapData{
					Lat:        pointLat,
					Lon:        pointLon,
					Likelihood: likelihood,
				})
			}
		}
	}

	log.Info("Heatmap data calculated", "datapoints", len(heatmapData))

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(heatmapData)
	if err != nil {
		log.Error("Error encoding JSON response", "error", err)
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
		return
	}
}

func main() {
	// Set logging level to Debug for detailed logs
	log.SetLevel(log.DebugLevel)
	log.Info("Initializing rainbow prediction server")
	log.Debug("API Key", "key", apiKey)
	r := mux.NewRouter()

	// Serve static files
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Debug("Serving index.html")
		http.ServeFile(w, r, "index.html")
	})

	// API route for prediction
	r.HandleFunc("/predict/{lat}/{lon}", handlePrediction).Methods("GET")

	// API route for heatmap data
	r.HandleFunc("/heatmap", handleHeatmapData).Methods("GET")

	// Start the server
	port := 8080
	log.Info("Server starting", "url", fmt.Sprintf("http://localhost:%d", port))
	log.Fatal("Server stopped", "error", http.ListenAndServe(fmt.Sprintf(":%d", port), r))
}
