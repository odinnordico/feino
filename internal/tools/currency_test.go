package tools

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── test helpers ──────────────────────────────────────────────────────────────

// fakeFrankfurter starts a test server, points frankfurterBase and
// currencyHTTPDoer at it, and returns a restore function.
func fakeFrankfurter(t *testing.T, handler http.HandlerFunc) (restore func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	origBase := frankfurterBase
	origDoer := currencyHTTPDoer
	frankfurterBase = srv.URL
	currencyHTTPDoer = srv.Client()
	return func() {
		frankfurterBase = origBase
		currencyHTTPDoer = origDoer
		srv.Close()
	}
}

// v2RatesResponse returns a JSON array of Rate records as the v2 API would.
func v2RatesResponse(records ...frankfurterRate) string {
	b, _ := json.Marshal(records)
	return string(b)
}

// ── currency_rates tests ──────────────────────────────────────────────────────

func TestCurrencyRates_LatestRates(t *testing.T) {
	restore := fakeFrankfurter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rates" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(v2RatesResponse(
			frankfurterRate{Date: "2024-01-15", Base: "USD", Quote: "EUR", Rate: 0.92},
			frankfurterRate{Date: "2024-01-15", Base: "USD", Quote: "GBP", Rate: 0.79},
		)))
	})
	defer restore()

	tool := newCurrencyRatesTool(slog.Default())
	result := tool.Run(map[string]any{"base": "USD", "quotes": "EUR,GBP"})

	if result.GetError() != nil {
		t.Fatalf("unexpected error: %v", result.GetError())
	}
	got, _ := result.GetContent().(string)
	if !strings.Contains(got, "EUR") {
		t.Errorf("expected EUR in rates, got %q", got)
	}
	if !strings.Contains(got, "USD") {
		t.Errorf("expected USD base in response, got %q", got)
	}
}

func TestCurrencyRates_QuotesParam(t *testing.T) {
	quotesReceived := ""
	restore := fakeFrankfurter(t, func(w http.ResponseWriter, r *http.Request) {
		quotesReceived = r.URL.Query().Get("quotes")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(v2RatesResponse(
			frankfurterRate{Date: "2024-01-15", Base: "EUR", Quote: "USD", Rate: 1.09},
		)))
	})
	defer restore()

	tool := newCurrencyRatesTool(slog.Default())
	tool.Run(map[string]any{"base": "EUR", "quotes": "USD,GBP"})

	if !strings.Contains(quotesReceived, "USD") {
		t.Errorf("quotes param: want USD, got %q", quotesReceived)
	}
}

func TestCurrencyRates_HistoricalDate(t *testing.T) {
	restore := fakeFrankfurter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("date") != "2023-06-01" {
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(v2RatesResponse(
			frankfurterRate{Date: "2023-06-01", Base: "USD", Quote: "EUR", Rate: 0.91},
		)))
	})
	defer restore()

	tool := newCurrencyRatesTool(slog.Default())
	result := tool.Run(map[string]any{"base": "USD", "date": "2023-06-01"})

	if result.GetError() != nil {
		t.Fatalf("unexpected error: %v", result.GetError())
	}
	got, _ := result.GetContent().(string)
	if !strings.Contains(got, "2023-06-01") {
		t.Errorf("expected historical date in response, got %q", got)
	}
}

func TestCurrencyRates_DateSentAsQueryParam(t *testing.T) {
	dateReceived := ""
	restore := fakeFrankfurter(t, func(w http.ResponseWriter, r *http.Request) {
		dateReceived = r.URL.Query().Get("date")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(v2RatesResponse(
			frankfurterRate{Date: "2023-06-01", Base: "EUR", Quote: "USD", Rate: 1.07},
		)))
	})
	defer restore()

	tool := newCurrencyRatesTool(slog.Default())
	tool.Run(map[string]any{"base": "EUR", "date": "2023-06-01"})

	if dateReceived != "2023-06-01" {
		t.Errorf("date query param: want 2023-06-01, got %q", dateReceived)
	}
}

func TestCurrencyRates_InvalidDate(t *testing.T) {
	tool := newCurrencyRatesTool(slog.Default())
	result := tool.Run(map[string]any{"base": "USD", "date": "not-a-date"})
	if result.GetError() == nil {
		t.Fatal("expected error for invalid date, got nil")
	}
}

func TestCurrencyRates_APIError(t *testing.T) {
	restore := fakeFrankfurter(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"currency not found"}`, http.StatusNotFound)
	})
	defer restore()

	tool := newCurrencyRatesTool(slog.Default())
	result := tool.Run(map[string]any{"base": "XYZ"})
	if result.GetError() == nil {
		t.Fatal("expected error for unknown currency, got nil")
	}
}

func TestCurrencyRates_RateMapShape(t *testing.T) {
	restore := fakeFrankfurter(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(v2RatesResponse(
			frankfurterRate{Date: "2024-01-15", Base: "EUR", Quote: "USD", Rate: 1.09},
			frankfurterRate{Date: "2024-01-15", Base: "EUR", Quote: "GBP", Rate: 0.86},
		)))
	})
	defer restore()

	tool := newCurrencyRatesTool(slog.Default())
	result := tool.Run(map[string]any{"base": "EUR"})

	got, _ := result.GetContent().(string)
	var out currencyRatesResult
	if err := json.Unmarshal([]byte(got), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Rates["USD"] != 1.09 {
		t.Errorf("USD rate: want 1.09, got %v", out.Rates["USD"])
	}
	if out.Rates["GBP"] != 0.86 {
		t.Errorf("GBP rate: want 0.86, got %v", out.Rates["GBP"])
	}
	if out.Date != "2024-01-15" {
		t.Errorf("date: want 2024-01-15, got %q", out.Date)
	}
}

// ── currency_convert tests ────────────────────────────────────────────────────

func TestCurrencyConvert_Basic(t *testing.T) {
	restore := fakeFrankfurter(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(v2RatesResponse(
			frankfurterRate{Date: "2024-01-15", Base: "USD", Quote: "EUR", Rate: 0.92},
		)))
	})
	defer restore()

	tool := newCurrencyConvertTool(slog.Default())
	result := tool.Run(map[string]any{"amount": float64(100), "from": "USD", "to": "EUR"})

	if result.GetError() != nil {
		t.Fatalf("unexpected error: %v", result.GetError())
	}
	got, _ := result.GetContent().(string)
	var out currencyConvertResult
	if err := json.Unmarshal([]byte(got), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Results) != 1 {
		t.Fatalf("results: want 1, got %d", len(out.Results))
	}
	if out.Results[0].To != "EUR" {
		t.Errorf("to: want EUR, got %q", out.Results[0].To)
	}
	if out.Results[0].Rate != 0.92 {
		t.Errorf("rate: want 0.92, got %v", out.Results[0].Rate)
	}
	// 100 × 0.92 = 92.0
	if out.Results[0].Converted != 92.0 {
		t.Errorf("converted: want 92.0, got %v", out.Results[0].Converted)
	}
}

func TestCurrencyConvert_MultipleTargets(t *testing.T) {
	restore := fakeFrankfurter(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(v2RatesResponse(
			frankfurterRate{Date: "2024-01-15", Base: "GBP", Quote: "USD", Rate: 1.27},
			frankfurterRate{Date: "2024-01-15", Base: "GBP", Quote: "EUR", Rate: 1.16},
		)))
	})
	defer restore()

	tool := newCurrencyConvertTool(slog.Default())
	result := tool.Run(map[string]any{"amount": float64(50), "from": "GBP", "to": "USD,EUR"})

	if result.GetError() != nil {
		t.Fatalf("unexpected error: %v", result.GetError())
	}
	got, _ := result.GetContent().(string)
	if !strings.Contains(got, "USD") || !strings.Contains(got, "EUR") {
		t.Errorf("expected both target currencies in response, got %q", got)
	}
}

func TestCurrencyConvert_ConvertedAmountCalculated(t *testing.T) {
	restore := fakeFrankfurter(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(v2RatesResponse(
			frankfurterRate{Date: "2024-01-15", Base: "USD", Quote: "JPY", Rate: 148.5},
		)))
	})
	defer restore()

	tool := newCurrencyConvertTool(slog.Default())
	result := tool.Run(map[string]any{"amount": float64(200), "from": "USD", "to": "JPY"})

	got, _ := result.GetContent().(string)
	var out currencyConvertResult
	_ = json.Unmarshal([]byte(got), &out)

	// 200 × 148.5 = 29700
	if out.Results[0].Converted != 29700.0 {
		t.Errorf("converted: want 29700.0, got %v", out.Results[0].Converted)
	}
}

func TestCurrencyConvert_NoAmountQueryParam(t *testing.T) {
	// The v2 API has no amount parameter — verify we do NOT send it.
	amountSeen := ""
	restore := fakeFrankfurter(t, func(w http.ResponseWriter, r *http.Request) {
		amountSeen = r.URL.Query().Get("amount")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(v2RatesResponse(
			frankfurterRate{Date: "2024-01-15", Base: "USD", Quote: "EUR", Rate: 0.92},
		)))
	})
	defer restore()

	tool := newCurrencyConvertTool(slog.Default())
	tool.Run(map[string]any{"amount": float64(100), "from": "USD", "to": "EUR"})

	if amountSeen != "" {
		t.Errorf("amount query param should NOT be sent to v2 API, but got %q", amountSeen)
	}
}

func TestCurrencyConvert_UsesBaseAndQuotesParams(t *testing.T) {
	baseReceived, quotesReceived := "", ""
	restore := fakeFrankfurter(t, func(w http.ResponseWriter, r *http.Request) {
		baseReceived = r.URL.Query().Get("base")
		quotesReceived = r.URL.Query().Get("quotes")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(v2RatesResponse(
			frankfurterRate{Date: "2024-01-15", Base: "USD", Quote: "EUR", Rate: 0.92},
		)))
	})
	defer restore()

	tool := newCurrencyConvertTool(slog.Default())
	tool.Run(map[string]any{"amount": float64(1), "from": "USD", "to": "EUR"})

	if baseReceived != "USD" {
		t.Errorf("base param: want USD, got %q", baseReceived)
	}
	if quotesReceived != "EUR" {
		t.Errorf("quotes param: want EUR, got %q", quotesReceived)
	}
}

func TestCurrencyConvert_MissingFrom(t *testing.T) {
	tool := newCurrencyConvertTool(slog.Default())
	result := tool.Run(map[string]any{"amount": float64(10), "to": "EUR"})
	if result.GetError() == nil {
		t.Fatal("expected error for missing 'from', got nil")
	}
}

func TestCurrencyConvert_MissingTo(t *testing.T) {
	tool := newCurrencyConvertTool(slog.Default())
	result := tool.Run(map[string]any{"amount": float64(10), "from": "USD"})
	if result.GetError() == nil {
		t.Fatal("expected error for missing 'to', got nil")
	}
}

func TestCurrencyConvert_MissingAmount(t *testing.T) {
	tool := newCurrencyConvertTool(slog.Default())
	result := tool.Run(map[string]any{"from": "USD", "to": "EUR"})
	if result.GetError() == nil {
		t.Fatal("expected error for missing amount, got nil")
	}
}

func TestCurrencyConvert_InvalidDate(t *testing.T) {
	tool := newCurrencyConvertTool(slog.Default())
	result := tool.Run(map[string]any{"amount": float64(1), "from": "USD", "to": "EUR", "date": "bad"})
	if result.GetError() == nil {
		t.Fatal("expected error for invalid date, got nil")
	}
}

func TestCurrencyConvert_HistoricalDate(t *testing.T) {
	restore := fakeFrankfurter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("date") != "2023-01-01" {
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(v2RatesResponse(
			frankfurterRate{Date: "2023-01-01", Base: "EUR", Quote: "USD", Rate: 1.07},
		)))
	})
	defer restore()

	tool := newCurrencyConvertTool(slog.Default())
	result := tool.Run(map[string]any{"amount": float64(1), "from": "EUR", "to": "USD", "date": "2023-01-01"})

	if result.GetError() != nil {
		t.Fatalf("unexpected error: %v", result.GetError())
	}
	got, _ := result.GetContent().(string)
	if !strings.Contains(got, "2023-01-01") {
		t.Errorf("expected historical date in response, got %q", got)
	}
}
