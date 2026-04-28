package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// frankfurterBase is a var so tests can redirect requests to a local httptest server.
var frankfurterBase = "https://api.frankfurter.dev/v2"

// currencyHTTPDoer is the HTTP client used by currency tools.
// Replaced in tests with a lightweight fake.
var currencyHTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
} = http.DefaultClient

const (
	currencyTimeout    = 10 * time.Second
	currencyDateFormat = "2006-01-02" // YYYY-MM-DD accepted by the API
)

// frankfurterRate is one record returned by the Frankfurter v2 /rates endpoint.
// The response is a JSON array: [{date, base, quote, rate}, …]
type frankfurterRate struct {
	Date  string  `json:"date"`
	Base  string  `json:"base"`
	Quote string  `json:"quote"`
	Rate  float64 `json:"rate"`
}

// ── agent-facing result shapes ────────────────────────────────────────────────

type currencyRatesResult struct {
	Base  string             `json:"base"`
	Date  string             `json:"date"`
	Rates map[string]float64 `json:"rates"`
}

type currencyConvertedPair struct {
	To        string  `json:"to"`
	Rate      float64 `json:"rate"`
	Converted float64 `json:"converted"`
}

type currencyConvertResult struct {
	From    string                  `json:"from"`
	Amount  float64                 `json:"amount"`
	Date    string                  `json:"date"`
	Results []currencyConvertedPair `json:"results"`
}

// ── NewCurrencyTools ──────────────────────────────────────────────────────────

// NewCurrencyTools returns the currency tool suite.
func NewCurrencyTools(logger *slog.Logger) []Tool {
	return []Tool{
		newCurrencyRatesTool(logger),
		newCurrencyConvertTool(logger),
	}
}

// ── frankfurterGet ────────────────────────────────────────────────────────────

// frankfurterGet performs a GET request against the Frankfurter v2 API
// (path must include the leading slash and any query string) and returns the
// parsed array of rate records.
func frankfurterGet(ctx context.Context, path string, logger *slog.Logger) ([]frankfurterRate, error) {
	safeLogger(logger).Debug("frankfurterGet", "url", frankfurterBase+path)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, frankfurterBase+path, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "feino-agent/1.0 (+https://github.com/odinnordico/feino)")

	log := safeLogger(logger)
	resp, err := currencyHTTPDoer.Do(req)
	if err != nil {
		log.Error("frankfurterGet: request failed", "error", err)
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	limited := io.LimitReader(resp.Body, 256*1024)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		log.Error("frankfurterGet: API error", "status", resp.StatusCode)
		var apiErr struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &apiErr)
		msg := apiErr.Message
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("frankfurter API: %s", msg)
	}

	var rates []frankfurterRate
	if err := json.Unmarshal(body, &rates); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return rates, nil
}

// ── currency_rates ────────────────────────────────────────────────────────────

func newCurrencyRatesTool(logger *slog.Logger) Tool {
	return NewTool(
		"currency_rates",
		"Get exchange rates for a base currency using the Frankfurter API (ECB data, no API key required). "+
			"Returns rates for the requested date or the latest available business day.",
		map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"base": map[string]any{
					"type":        "string",
					"description": "Base currency code (ISO 4217), e.g. USD, EUR, GBP. Defaults to EUR.",
				},
				"quotes": map[string]any{
					"type":        "string",
					"description": `Comma-separated target currency codes, e.g. "USD,GBP,JPY". Omit to return all available currencies.`,
				},
				"date": map[string]any{
					"type":        "string",
					"description": "Historical date in YYYY-MM-DD format. Omit for latest rates.",
				},
			},
		},
		func(params map[string]any) ToolResult {
			base := strings.ToUpper(getStringDefault(params, "base", "EUR"))
			quotes := strings.ToUpper(getStringDefault(params, "quotes", ""))
			date := getStringDefault(params, "date", "")

			if date != "" {
				if _, err := time.Parse(currencyDateFormat, date); err != nil {
					return NewToolResult("", fmt.Errorf("currency_rates: invalid date %q, expected YYYY-MM-DD", date))
				}
			}

			q := url.Values{}
			q.Set("base", base)
			if quotes != "" {
				q.Set("quotes", quotes)
			}
			if date != "" {
				q.Set("date", date)
			}

			safeLogger(logger).Debug("currency_rates", "base", base, "quotes", quotes, "date", date)

			ctx, cancel := context.WithTimeout(context.Background(), currencyTimeout)
			defer cancel()

			rates, err := frankfurterGet(ctx, "/rates?"+q.Encode(), logger)
			if err != nil {
				return NewToolResult("", fmt.Errorf("currency_rates: %w", err))
			}

			// Convert array to the cleaner map form for the agent.
			result := currencyRatesResult{
				Base:  base,
				Rates: make(map[string]float64, len(rates)),
			}
			for _, r := range rates {
				result.Date = r.Date // all records share the same date
				result.Rates[r.Quote] = r.Rate
			}

			out, _ := json.MarshalIndent(result, "", "  ")
			return NewToolResult(string(out), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// ── currency_convert ──────────────────────────────────────────────────────────

func newCurrencyConvertTool(logger *slog.Logger) Tool {
	return NewTool(
		"currency_convert",
		"Convert an amount from one currency to one or more target currencies using ECB rates via Frankfurter (no API key required). "+
			"Returns the exchange rate and converted amount for each target.",
		map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"amount": map[string]any{
					"type":        "number",
					"description": "Amount to convert.",
				},
				"from": map[string]any{
					"type":        "string",
					"description": "Source currency code (ISO 4217), e.g. USD.",
				},
				"to": map[string]any{
					"type":        "string",
					"description": `Target currency code (ISO 4217), e.g. EUR. Use a comma-separated list for multiple targets, e.g. "EUR,GBP,JPY".`,
				},
				"date": map[string]any{
					"type":        "string",
					"description": "Historical date in YYYY-MM-DD format. Omit for latest rates.",
				},
			},
			"required": []string{"amount", "from", "to"},
		},
		func(params map[string]any) ToolResult {
			from := strings.ToUpper(getStringDefault(params, "from", ""))
			to := strings.ToUpper(getStringDefault(params, "to", ""))
			date := getStringDefault(params, "date", "")

			if from == "" {
				return NewToolResult("", fmt.Errorf("currency_convert: 'from' parameter is required"))
			}
			if to == "" {
				return NewToolResult("", fmt.Errorf("currency_convert: 'to' parameter is required"))
			}

			// amount may arrive as float64 or int from JSON decoding.
			var amount float64
			switch v := params["amount"].(type) {
			case float64:
				amount = v
			case int:
				amount = float64(v)
			default:
				return NewToolResult("", fmt.Errorf("currency_convert: 'amount' parameter is required"))
			}

			if date != "" {
				if _, err := time.Parse(currencyDateFormat, date); err != nil {
					return NewToolResult("", fmt.Errorf("currency_convert: invalid date %q, expected YYYY-MM-DD", date))
				}
			}

			// v2 API has no amount parameter — fetch rates then multiply.
			q := url.Values{}
			q.Set("base", from)
			q.Set("quotes", to)
			if date != "" {
				q.Set("date", date)
			}

			safeLogger(logger).Debug("currency_convert", "amount", amount, "from", from, "to", to, "date", date)

			ctx, cancel := context.WithTimeout(context.Background(), currencyTimeout)
			defer cancel()

			rates, err := frankfurterGet(ctx, "/rates?"+q.Encode(), logger)
			if err != nil {
				return NewToolResult("", fmt.Errorf("currency_convert: %w", err))
			}

			result := currencyConvertResult{
				From:    from,
				Amount:  amount,
				Results: make([]currencyConvertedPair, 0, len(rates)),
			}
			for _, r := range rates {
				result.Date = r.Date
				result.Results = append(result.Results, currencyConvertedPair{
					To:        r.Quote,
					Rate:      r.Rate,
					Converted: roundFloat(amount*r.Rate, 4),
				})
			}

			out, _ := json.MarshalIndent(result, "", "  ")
			return NewToolResult(string(out), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}
