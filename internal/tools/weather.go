package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	weatherDefaultDays = 7
	weatherMaxDays     = 16
	weatherTimeoutSec  = 10
)

var (
	weatherGeoBaseURL      = "https://geocoding-api.open-meteo.com/v1/search"
	weatherForecastBaseURL = "https://api.open-meteo.com/v1/forecast"
	weatherHTTPDoer        interface {
		Do(*http.Request) (*http.Response, error)
	} = http.DefaultClient
)

// ── Open-Meteo API response shapes ───────────────────────────────────────────

type geoResponse struct {
	Results []geoResult `json:"results"`
}

type geoResult struct {
	Name      string  `json:"name"`
	Country   string  `json:"country"`
	Admin1    string  `json:"admin1"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timezone  string  `json:"timezone"`
}

type omCurrentResponse struct {
	Latitude  float64        `json:"latitude"`
	Longitude float64        `json:"longitude"`
	Timezone  string         `json:"timezone"`
	Current   omCurrentData  `json:"current"`
	Units     omCurrentUnits `json:"current_units"`
}

type omCurrentData struct {
	Time          string  `json:"time"`
	Temperature   float64 `json:"temperature_2m"`
	FeelsLike     float64 `json:"apparent_temperature"`
	Humidity      int     `json:"relative_humidity_2m"`
	Precipitation float64 `json:"precipitation"`
	WeatherCode   int     `json:"weather_code"`
	WindSpeed     float64 `json:"wind_speed_10m"`
	WindDirection int     `json:"wind_direction_10m"`
}

type omCurrentUnits struct {
	Temperature string `json:"temperature_2m"`
	WindSpeed   string `json:"wind_speed_10m"`
}

type omForecastResponse struct {
	Latitude  float64      `json:"latitude"`
	Longitude float64      `json:"longitude"`
	Timezone  string       `json:"timezone"`
	Daily     omDailyData  `json:"daily"`
	Units     omDailyUnits `json:"daily_units"`
}

type omDailyData struct {
	Time          []string  `json:"time"`
	MaxTemp       []float64 `json:"temperature_2m_max"`
	MinTemp       []float64 `json:"temperature_2m_min"`
	Precipitation []float64 `json:"precipitation_sum"`
	WeatherCode   []int     `json:"weather_code"`
	MaxWindSpeed  []float64 `json:"wind_speed_10m_max"`
}

type omDailyUnits struct {
	Temperature string `json:"temperature_2m_max"`
	WindSpeed   string `json:"wind_speed_10m_max"`
}

// ── agent-facing result types ─────────────────────────────────────────────────

type weatherCurrentResult struct {
	Location      string  `json:"location"`
	Latitude      float64 `json:"latitude"`
	Longitude     float64 `json:"longitude"`
	Timezone      string  `json:"timezone"`
	Time          string  `json:"time"`
	Temperature   float64 `json:"temperature"`
	FeelsLike     float64 `json:"feels_like"`
	HumidityPct   int     `json:"humidity_pct"`
	PrecipMM      float64 `json:"precipitation_mm"`
	WindSpeed     float64 `json:"wind_speed"`
	WindDirDeg    int     `json:"wind_direction_deg"`
	Condition     string  `json:"condition"`
	Units         string  `json:"units"`
	WindSpeedUnit string  `json:"wind_speed_unit"`
}

type weatherForecastDay struct {
	Date         string  `json:"date"`
	MaxTemp      float64 `json:"max_temp"`
	MinTemp      float64 `json:"min_temp"`
	PrecipMM     float64 `json:"precipitation_mm"`
	MaxWindSpeed float64 `json:"max_wind_speed"`
	Condition    string  `json:"condition"`
}

type weatherForecastResult struct {
	Location      string               `json:"location"`
	Latitude      float64              `json:"latitude"`
	Longitude     float64              `json:"longitude"`
	Timezone      string               `json:"timezone"`
	Units         string               `json:"units"`
	WindSpeedUnit string               `json:"wind_speed_unit"`
	Days          []weatherForecastDay `json:"days"`
}

// ── NewWeatherTools ───────────────────────────────────────────────────────────

// NewWeatherTools returns the weather_current and weather_forecast tools.
func NewWeatherTools(logger *slog.Logger) []Tool {
	return []Tool{
		newWeatherCurrentTool(logger),
		newWeatherForecastTool(logger),
	}
}

// ── weather_current ───────────────────────────────────────────────────────────

func newWeatherCurrentTool(logger *slog.Logger) Tool {
	return NewTool(
		"weather_current",
		"Get the current weather for a location. "+
			"Returns temperature, feels-like, humidity, precipitation, wind speed/direction, and a human-readable condition description. "+
			"Location can be a city name (e.g. \"Paris\", \"Tokyo, Japan\") or a \"lat,lon\" coordinate pair.",
		map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"location": map[string]any{
					"type":        "string",
					"description": "City name (e.g. \"London\", \"New York, US\") or \"lat,lon\" (e.g. \"48.85,2.35\").",
				},
				"units": map[string]any{
					"type":        "string",
					"description": `Temperature unit: "celsius" (default) or "fahrenheit".`,
					"enum":        []string{"celsius", "fahrenheit"},
				},
			},
			"required": []string{"location"},
		},
		func(params map[string]any) ToolResult {
			location, ok := getString(params, "location")
			if !ok || strings.TrimSpace(location) == "" {
				return NewToolResult("", fmt.Errorf("weather_current: location is required"))
			}
			units := strings.ToLower(getStringDefault(params, "units", "celsius"))
			if units != "celsius" && units != "fahrenheit" {
				units = "celsius"
			}

			ctx, cancel := context.WithTimeout(context.Background(), weatherTimeoutSec*time.Second)
			defer cancel()

			lat, lon, displayName, err := geocode(ctx, location)
			if err != nil {
				return NewToolResult("", fmt.Errorf("weather_current: geocode %q: %w", location, err))
			}

			omResp, err := fetchCurrentWeather(ctx, lat, lon, units)
			if err != nil {
				return NewToolResult("", fmt.Errorf("weather_current: fetch: %w", err))
			}

			result := weatherCurrentResult{
				Location:      displayName,
				Latitude:      omResp.Latitude,
				Longitude:     omResp.Longitude,
				Timezone:      omResp.Timezone,
				Time:          omResp.Current.Time,
				Temperature:   omResp.Current.Temperature,
				FeelsLike:     omResp.Current.FeelsLike,
				HumidityPct:   omResp.Current.Humidity,
				PrecipMM:      omResp.Current.Precipitation,
				WindSpeed:     omResp.Current.WindSpeed,
				WindDirDeg:    omResp.Current.WindDirection,
				Condition:     wmoCondition(omResp.Current.WeatherCode),
				Units:         units,
				WindSpeedUnit: omResp.Units.WindSpeed,
			}

			safeLogger(logger).Debug("weather_current", "location", displayName, "code", omResp.Current.WeatherCode)

			out, _ := json.MarshalIndent(result, "", "  ")
			return NewToolResult(string(out), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// ── weather_forecast ──────────────────────────────────────────────────────────

func newWeatherForecastTool(logger *slog.Logger) Tool {
	return NewTool(
		"weather_forecast",
		"Get a multi-day weather forecast for a location. "+
			"Returns daily max/min temperature, precipitation sum, wind speed, and condition for each day. "+
			"Location can be a city name or a \"lat,lon\" coordinate pair.",
		map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"location": map[string]any{
					"type":        "string",
					"description": "City name (e.g. \"Berlin\") or \"lat,lon\" (e.g. \"52.52,13.41\").",
				},
				"days": map[string]any{
					"type":        "integer",
					"description": fmt.Sprintf("Number of days to forecast (1–%d). Defaults to %d.", weatherMaxDays, weatherDefaultDays),
				},
				"units": map[string]any{
					"type":        "string",
					"description": `Temperature unit: "celsius" (default) or "fahrenheit".`,
					"enum":        []string{"celsius", "fahrenheit"},
				},
			},
			"required": []string{"location"},
		},
		func(params map[string]any) ToolResult {
			location, ok := getString(params, "location")
			if !ok || strings.TrimSpace(location) == "" {
				return NewToolResult("", fmt.Errorf("weather_forecast: location is required"))
			}
			units := strings.ToLower(getStringDefault(params, "units", "celsius"))
			if units != "celsius" && units != "fahrenheit" {
				units = "celsius"
			}
			days := min(max(getInt(params, "days", weatherDefaultDays), 1), weatherMaxDays)

			ctx, cancel := context.WithTimeout(context.Background(), weatherTimeoutSec*time.Second)
			defer cancel()

			lat, lon, displayName, err := geocode(ctx, location)
			if err != nil {
				return NewToolResult("", fmt.Errorf("weather_forecast: geocode %q: %w", location, err))
			}

			omResp, err := fetchForecast(ctx, lat, lon, units, days)
			if err != nil {
				return NewToolResult("", fmt.Errorf("weather_forecast: fetch: %w", err))
			}

			d := omResp.Daily
			nDays := len(d.Time)
			forecastDays := make([]weatherForecastDay, 0, nDays)
			for i := range nDays {
				day := weatherForecastDay{Date: safeStr(d.Time, i)}
				if i < len(d.MaxTemp) {
					day.MaxTemp = d.MaxTemp[i]
				}
				if i < len(d.MinTemp) {
					day.MinTemp = d.MinTemp[i]
				}
				if i < len(d.Precipitation) {
					day.PrecipMM = d.Precipitation[i]
				}
				if i < len(d.WeatherCode) {
					day.Condition = wmoCondition(d.WeatherCode[i])
				}
				if i < len(d.MaxWindSpeed) {
					day.MaxWindSpeed = d.MaxWindSpeed[i]
				}
				forecastDays = append(forecastDays, day)
			}

			result := weatherForecastResult{
				Location:      displayName,
				Latitude:      omResp.Latitude,
				Longitude:     omResp.Longitude,
				Timezone:      omResp.Timezone,
				Units:         units,
				WindSpeedUnit: omResp.Units.WindSpeed,
				Days:          forecastDays,
			}

			safeLogger(logger).Debug("weather_forecast", "location", displayName, "days", nDays)

			out, _ := json.MarshalIndent(result, "", "  ")
			return NewToolResult(string(out), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// ── shared helpers ────────────────────────────────────────────────────────────

// geocode resolves a location string to latitude, longitude, and a display name.
// Accepts "lat,lon" directly or a city name / "City, Country" string.
func geocode(ctx context.Context, location string) (lat, lon float64, name string, err error) {
	// Try "lat,lon" numeric format first.
	if parts := strings.SplitN(location, ",", 2); len(parts) == 2 {
		if la, e1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64); e1 == nil {
			if lo, e2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64); e2 == nil {
				return la, lo, location, nil
			}
		}
	}

	q := url.Values{}
	q.Set("name", location)
	q.Set("count", "1")
	q.Set("language", "en")
	q.Set("format", "json")
	reqURL := weatherGeoBaseURL + "?" + q.Encode()

	body, err := weatherGet(ctx, reqURL)
	if err != nil {
		return 0, 0, "", fmt.Errorf("geocoding API: %w", err)
	}

	var gr geoResponse
	if err := json.Unmarshal(body, &gr); err != nil {
		return 0, 0, "", fmt.Errorf("geocoding API: parse response: %w", err)
	}
	if len(gr.Results) == 0 {
		return 0, 0, "", fmt.Errorf("location %q not found", location)
	}
	r := gr.Results[0]
	displayName := r.Name
	if r.Admin1 != "" {
		displayName += ", " + r.Admin1
	}
	if r.Country != "" {
		displayName += ", " + r.Country
	}
	return r.Latitude, r.Longitude, displayName, nil
}

// fetchCurrentWeather calls the Open-Meteo current endpoint.
func fetchCurrentWeather(ctx context.Context, lat, lon float64, units string) (*omCurrentResponse, error) {
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(lat, 'f', 6, 64))
	q.Set("longitude", strconv.FormatFloat(lon, 'f', 6, 64))
	q.Set("current", "temperature_2m,apparent_temperature,relative_humidity_2m,precipitation,weather_code,wind_speed_10m,wind_direction_10m")
	q.Set("timezone", "auto")
	q.Set("temperature_unit", units)
	if units == "fahrenheit" {
		q.Set("wind_speed_unit", "mph")
	} else {
		q.Set("wind_speed_unit", "kmh")
	}

	body, err := weatherGet(ctx, weatherForecastBaseURL+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	var resp omCurrentResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse current weather: %w", err)
	}
	return &resp, nil
}

// fetchForecast calls the Open-Meteo daily forecast endpoint.
func fetchForecast(ctx context.Context, lat, lon float64, units string, days int) (*omForecastResponse, error) {
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(lat, 'f', 6, 64))
	q.Set("longitude", strconv.FormatFloat(lon, 'f', 6, 64))
	q.Set("daily", "temperature_2m_max,temperature_2m_min,precipitation_sum,weather_code,wind_speed_10m_max")
	q.Set("timezone", "auto")
	q.Set("forecast_days", strconv.Itoa(days))
	q.Set("temperature_unit", units)
	if units == "fahrenheit" {
		q.Set("wind_speed_unit", "mph")
	} else {
		q.Set("wind_speed_unit", "kmh")
	}

	body, err := weatherGet(ctx, weatherForecastBaseURL+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	var resp omForecastResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse forecast: %w", err)
	}
	return &resp, nil
}

// weatherGet performs a GET request via weatherHTTPDoer and returns the body bytes.
func weatherGet(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "feino-agent/1.0 (+https://github.com/odinnordico/feino)")

	resp, err := weatherHTTPDoer.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, 256*1024)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// safeStr returns slice[i] or "" if out of bounds.
func safeStr(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}

// wmoCondition maps a WMO weather interpretation code to a human description.
func wmoCondition(code int) string {
	switch code {
	case 0:
		return "Clear sky"
	case 1:
		return "Mainly clear"
	case 2:
		return "Partly cloudy"
	case 3:
		return "Overcast"
	case 45:
		return "Fog"
	case 48:
		return "Icy fog"
	case 51:
		return "Light drizzle"
	case 53:
		return "Moderate drizzle"
	case 55:
		return "Dense drizzle"
	case 56:
		return "Light freezing drizzle"
	case 57:
		return "Dense freezing drizzle"
	case 61:
		return "Slight rain"
	case 63:
		return "Moderate rain"
	case 65:
		return "Heavy rain"
	case 66:
		return "Light freezing rain"
	case 67:
		return "Heavy freezing rain"
	case 71:
		return "Slight snow"
	case 73:
		return "Moderate snow"
	case 75:
		return "Heavy snow"
	case 77:
		return "Snow grains"
	case 80:
		return "Slight rain showers"
	case 81:
		return "Moderate rain showers"
	case 82:
		return "Violent rain showers"
	case 85:
		return "Slight snow showers"
	case 86:
		return "Heavy snow showers"
	case 95:
		return "Thunderstorm"
	case 96:
		return "Thunderstorm with slight hail"
	case 99:
		return "Thunderstorm with heavy hail"
	default:
		return fmt.Sprintf("Unknown (code %d)", code)
	}
}
