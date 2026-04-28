package tools

import (
	"io"
	"log/slog"
)

// safeLogger returns l if non-nil, otherwise a discard logger.
func safeLogger(l *slog.Logger) *slog.Logger {
	if l != nil {
		return l
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// getString extracts a string parameter from the params map.
// Returns ("", false) if the key is absent or the value is not a string.
func getString(params map[string]any, key string) (string, bool) {
	v, ok := params[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// getStringDefault extracts a string parameter or returns a default value.
func getStringDefault(params map[string]any, key, def string) string {
	if s, ok := getString(params, key); ok {
		return s
	}
	return def
}

// getInt extracts an integer parameter from the params map.
// Handles int, int64, and float64 — JSON numbers decoded into map[string]any
// arrive as float64, so all three cases must be covered.
// Returns defaultVal if the key is absent or the value cannot be cast.
func getInt(params map[string]any, key string, defaultVal int) int {
	v, ok := params[key]
	if !ok {
		return defaultVal
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return defaultVal
}

// getFloat extracts a float64 parameter from the params map.
// Handles float64, int, and int64 — JSON numbers in map[string]any are float64.
// Returns defaultVal if the key is absent or the value cannot be cast.
func getFloat(params map[string]any, key string, defaultVal float64) float64 {
	v, ok := params[key]
	if !ok {
		return defaultVal
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return defaultVal
}

// getBool extracts a boolean parameter from the params map.
// Returns defaultVal if the key is absent or the value is not a bool.
func getBool(params map[string]any, key string, defaultVal bool) bool {
	v, ok := params[key]
	if !ok {
		return defaultVal
	}
	b, ok := v.(bool)
	if !ok {
		return defaultVal
	}
	return b
}
