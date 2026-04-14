package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── test helpers ──────────────────────────────────────────────────────────────

// withWeatherDoer swaps weatherHTTPDoer for the duration of the test.
func withWeatherDoer(t *testing.T, c *http.Client) {
	t.Helper()
	orig := weatherHTTPDoer
	weatherHTTPDoer = c
	t.Cleanup(func() { weatherHTTPDoer = orig })
}

// weatherServer builds a test server that routes /geo → geoHandler and
// /forecast → forecastHandler, then installs it as both geo and forecast
// base URLs.  Returns a cleanup function.
func weatherServer(t *testing.T, geoBody, forecastBody string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/geo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(geoBody))
	})
	mux.HandleFunc("/forecast", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(forecastBody))
	})
	srv := httptest.NewServer(mux)

	origGeo := weatherGeoBaseURL
	origForecast := weatherForecastBaseURL
	weatherGeoBaseURL = srv.URL + "/geo"
	weatherForecastBaseURL = srv.URL + "/forecast"
	withWeatherDoer(t, srv.Client())
	t.Cleanup(func() {
		weatherGeoBaseURL = origGeo
		weatherForecastBaseURL = origForecast
		srv.Close()
	})
	return srv
}

// ── geo fixtures ──────────────────────────────────────────────────────────────

const parisGeoJSON = `{
  "results": [{
    "name": "Paris",
    "country": "France",
    "admin1": "Île-de-France",
    "latitude": 48.853,
    "longitude": 2.349,
    "timezone": "Europe/Paris"
  }]
}`

const emptyGeoJSON = `{"results": []}`

// ── current-weather fixture ───────────────────────────────────────────────────

const currentWeatherJSON = `{
  "latitude": 48.853,
  "longitude": 2.349,
  "timezone": "Europe/Paris",
  "current": {
    "time": "2024-01-15T14:00",
    "temperature_2m": 7.2,
    "apparent_temperature": 4.1,
    "relative_humidity_2m": 82,
    "precipitation": 0.0,
    "weather_code": 3,
    "wind_speed_10m": 18.5,
    "wind_direction_10m": 220
  },
  "current_units": {
    "temperature_2m": "°C",
    "wind_speed_10m": "km/h"
  }
}`

// ── forecast fixture ──────────────────────────────────────────────────────────

const forecastJSON = `{
  "latitude": 48.853,
  "longitude": 2.349,
  "timezone": "Europe/Paris",
  "daily": {
    "time": ["2024-01-15", "2024-01-16", "2024-01-17"],
    "temperature_2m_max": [9.1, 8.4, 7.0],
    "temperature_2m_min": [4.2, 3.1, 2.5],
    "precipitation_sum": [0.0, 2.5, 5.1],
    "weather_code": [3, 63, 65],
    "wind_speed_10m_max": [22.1, 35.6, 40.2]
  },
  "daily_units": {
    "temperature_2m_max": "°C",
    "wind_speed_10m_max": "km/h"
  }
}`

// ── weather_current tests ─────────────────────────────────────────────────────

func callWeatherCurrent(params map[string]any) weatherCurrentResult {
	tool := newWeatherCurrentTool(nil)
	res := tool.Run(params)
	content, _ := res.GetContent().(string)
	var out weatherCurrentResult
	_ = json.Unmarshal([]byte(content), &out)
	return out
}

func callWeatherCurrentRaw(params map[string]any) string {
	tool := newWeatherCurrentTool(nil)
	res := tool.Run(params)
	if err := res.GetError(); err != nil {
		return err.Error()
	}
	s, _ := res.GetContent().(string)
	return s
}

func TestWeatherCurrent_CityName(t *testing.T) {
	weatherServer(t, parisGeoJSON, currentWeatherJSON)

	r := callWeatherCurrent(map[string]any{"location": "Paris"})
	if !strings.HasPrefix(r.Location, "Paris") {
		t.Errorf("location: want prefix Paris, got %q", r.Location)
	}
	if r.Temperature != 7.2 {
		t.Errorf("temperature: want 7.2, got %v", r.Temperature)
	}
	if r.FeelsLike != 4.1 {
		t.Errorf("feels_like: want 4.1, got %v", r.FeelsLike)
	}
	if r.HumidityPct != 82 {
		t.Errorf("humidity_pct: want 82, got %d", r.HumidityPct)
	}
	if r.Condition != "Overcast" {
		t.Errorf("condition: want Overcast (code 3), got %q", r.Condition)
	}
	if r.Units != "celsius" {
		t.Errorf("units: want celsius, got %q", r.Units)
	}
}

func TestWeatherCurrent_LatLon(t *testing.T) {
	// When the location is "lat,lon", geocoding is skipped entirely.
	// Point the forecast URL at a server that always returns currentWeatherJSON;
	// the geo endpoint must NOT be called.
	mux := http.NewServeMux()
	geoCalled := false
	mux.HandleFunc("/geo", func(w http.ResponseWriter, _ *http.Request) {
		geoCalled = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(parisGeoJSON))
	})
	mux.HandleFunc("/forecast", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(currentWeatherJSON))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	origGeo := weatherGeoBaseURL
	origForecast := weatherForecastBaseURL
	weatherGeoBaseURL = srv.URL + "/geo"
	weatherForecastBaseURL = srv.URL + "/forecast"
	withWeatherDoer(t, srv.Client())
	t.Cleanup(func() {
		weatherGeoBaseURL = origGeo
		weatherForecastBaseURL = origForecast
	})

	r := callWeatherCurrent(map[string]any{"location": "48.853,2.349"})
	if geoCalled {
		t.Error("geocoding API should NOT be called for lat,lon input")
	}
	if r.Temperature != 7.2 {
		t.Errorf("temperature: want 7.2, got %v", r.Temperature)
	}
}

func TestWeatherCurrent_Fahrenheit(t *testing.T) {
	weatherServer(t, parisGeoJSON, currentWeatherJSON)

	r := callWeatherCurrent(map[string]any{"location": "Paris", "units": "fahrenheit"})
	if r.Units != "fahrenheit" {
		t.Errorf("units: want fahrenheit, got %q", r.Units)
	}
}

func TestWeatherCurrent_LocationNotFound(t *testing.T) {
	weatherServer(t, emptyGeoJSON, currentWeatherJSON)

	raw := callWeatherCurrentRaw(map[string]any{"location": "NonExistentCity"})
	if !strings.Contains(raw, "not found") {
		t.Errorf("expected 'not found' in error, got: %s", raw)
	}
}

func TestWeatherCurrent_MissingLocation(t *testing.T) {
	raw := callWeatherCurrentRaw(map[string]any{})
	if !strings.Contains(raw, "location is required") {
		t.Errorf("expected 'location is required', got: %s", raw)
	}
}

func TestWeatherCurrent_WindFields(t *testing.T) {
	weatherServer(t, parisGeoJSON, currentWeatherJSON)

	r := callWeatherCurrent(map[string]any{"location": "Paris"})
	if r.WindSpeed != 18.5 {
		t.Errorf("wind_speed: want 18.5, got %v", r.WindSpeed)
	}
	if r.WindDirDeg != 220 {
		t.Errorf("wind_direction_deg: want 220, got %d", r.WindDirDeg)
	}
}

// ── weather_forecast tests ────────────────────────────────────────────────────

func callWeatherForecast(params map[string]any) weatherForecastResult {
	tool := newWeatherForecastTool(nil)
	res := tool.Run(params)
	content, _ := res.GetContent().(string)
	var out weatherForecastResult
	_ = json.Unmarshal([]byte(content), &out)
	return out
}

func callWeatherForecastRaw(params map[string]any) string {
	tool := newWeatherForecastTool(nil)
	res := tool.Run(params)
	if err := res.GetError(); err != nil {
		return err.Error()
	}
	s, _ := res.GetContent().(string)
	return s
}

func TestWeatherForecast_DefaultDays(t *testing.T) {
	weatherServer(t, parisGeoJSON, forecastJSON)

	r := callWeatherForecast(map[string]any{"location": "Paris"})
	if !strings.HasPrefix(r.Location, "Paris") {
		t.Errorf("location: want Paris prefix, got %q", r.Location)
	}
	if len(r.Days) != 3 {
		t.Errorf("days: want 3 (fixture has 3), got %d", len(r.Days))
	}
}

func TestWeatherForecast_DayFields(t *testing.T) {
	weatherServer(t, parisGeoJSON, forecastJSON)

	r := callWeatherForecast(map[string]any{"location": "Paris"})
	if len(r.Days) == 0 {
		t.Fatal("no days in forecast")
	}
	d0 := r.Days[0]
	if d0.Date != "2024-01-15" {
		t.Errorf("day[0].date: want 2024-01-15, got %q", d0.Date)
	}
	if d0.MaxTemp != 9.1 {
		t.Errorf("day[0].max_temp: want 9.1, got %v", d0.MaxTemp)
	}
	if d0.MinTemp != 4.2 {
		t.Errorf("day[0].min_temp: want 4.2, got %v", d0.MinTemp)
	}
	if d0.Condition != "Overcast" {
		t.Errorf("day[0].condition: want Overcast (code 3), got %q", d0.Condition)
	}
	if d0.PrecipMM != 0.0 {
		t.Errorf("day[0].precipitation_mm: want 0.0, got %v", d0.PrecipMM)
	}
}

func TestWeatherForecast_ConditionMapping(t *testing.T) {
	weatherServer(t, parisGeoJSON, forecastJSON)

	r := callWeatherForecast(map[string]any{"location": "Paris"})
	if len(r.Days) < 3 {
		t.Fatal("expected at least 3 days")
	}
	// day[1] has code 63 = "Moderate rain"
	if r.Days[1].Condition != "Moderate rain" {
		t.Errorf("day[1].condition: want 'Moderate rain', got %q", r.Days[1].Condition)
	}
	// day[2] has code 65 = "Heavy rain"
	if r.Days[2].Condition != "Heavy rain" {
		t.Errorf("day[2].condition: want 'Heavy rain', got %q", r.Days[2].Condition)
	}
}

func TestWeatherForecast_DaysClamp(t *testing.T) {
	// days > 16 should clamp to 16; the server receives forecast_days param.
	paramsCaptured := ""
	mux := http.NewServeMux()
	mux.HandleFunc("/geo", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(parisGeoJSON))
	})
	mux.HandleFunc("/forecast", func(w http.ResponseWriter, r *http.Request) {
		paramsCaptured = r.URL.Query().Get("forecast_days")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(forecastJSON))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	origGeo := weatherGeoBaseURL
	origForecast := weatherForecastBaseURL
	weatherGeoBaseURL = srv.URL + "/geo"
	weatherForecastBaseURL = srv.URL + "/forecast"
	withWeatherDoer(t, srv.Client())
	t.Cleanup(func() {
		weatherGeoBaseURL = origGeo
		weatherForecastBaseURL = origForecast
	})

	callWeatherForecast(map[string]any{"location": "Paris", "days": 99})
	if paramsCaptured != "16" {
		t.Errorf("forecast_days param: want 16, got %q", paramsCaptured)
	}
}

func TestWeatherForecast_MissingLocation(t *testing.T) {
	raw := callWeatherForecastRaw(map[string]any{})
	if !strings.Contains(raw, "location is required") {
		t.Errorf("expected 'location is required', got: %s", raw)
	}
}

func TestWeatherForecast_LocationNotFound(t *testing.T) {
	weatherServer(t, emptyGeoJSON, forecastJSON)

	raw := callWeatherForecastRaw(map[string]any{"location": "NoWhere"})
	if !strings.Contains(raw, "not found") {
		t.Errorf("expected 'not found', got: %s", raw)
	}
}

// ── WMO condition mapping ─────────────────────────────────────────────────────

func TestWMOCondition_KnownCodes(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{0, "Clear sky"},
		{1, "Mainly clear"},
		{2, "Partly cloudy"},
		{3, "Overcast"},
		{45, "Fog"},
		{48, "Icy fog"},
		{51, "Light drizzle"},
		{61, "Slight rain"},
		{63, "Moderate rain"},
		{65, "Heavy rain"},
		{71, "Slight snow"},
		{80, "Slight rain showers"},
		{95, "Thunderstorm"},
		{99, "Thunderstorm with heavy hail"},
	}
	for _, tc := range cases {
		got := wmoCondition(tc.code)
		if got != tc.want {
			t.Errorf("wmoCondition(%d): want %q, got %q", tc.code, tc.want, got)
		}
	}
}

func TestWMOCondition_UnknownCode(t *testing.T) {
	got := wmoCondition(999)
	if !strings.Contains(got, "999") {
		t.Errorf("unknown code should mention the code, got %q", got)
	}
}

// ── permission level ──────────────────────────────────────────────────────────

func TestWeatherTools_PermissionLevel(t *testing.T) {
	for _, tool := range NewWeatherTools(nil) {
		c, ok := tool.(Classified)
		if !ok {
			t.Errorf("%s: does not implement Classified", tool.GetName())
			continue
		}
		if c.PermissionLevel() != PermLevelRead {
			t.Errorf("%s: want PermLevelRead (%d), got %d", tool.GetName(), PermLevelRead, c.PermissionLevel())
		}
	}
}

// ── native tools registration ─────────────────────────────────────────────────

func TestNewNativeTools_IncludesWeather(t *testing.T) {
	tools := NewNativeTools(nil)
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.GetName()] = true
	}
	for _, want := range []string{"weather_current", "weather_forecast"} {
		if !names[want] {
			t.Errorf("%q not found in NewNativeTools output", want)
		}
	}
}
