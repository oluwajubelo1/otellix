package otellix

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// pricingDoc is the JSON shape accepted by SyncPricingFromJSON. Prices are USD
// per 1 million tokens.
//
//	{
//	  "openai/gpt-4o": {"input": 2.50, "output": 10.0, "cache_read": 1.25, "cache_write": 2.50}
//	}
type pricingDoc map[string]struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

// SyncPricingFromJSON merges a pricing document into the pricing table. It is
// additive: entries not present in data are left unchanged, and existing
// entries are overridden. Handy for hot-reloading prices from a config file.
func SyncPricingFromJSON(data []byte) error {
	var doc pricingDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("syncpricing: invalid JSON: %w", err)
	}
	for key, p := range doc {
		provider, model, err := splitPricingKey(key)
		if err != nil {
			return err
		}
		RegisterModel(provider, model, PricingEntry{
			InputPricePerMToken:      p.Input,
			OutputPricePerMToken:     p.Output,
			CacheReadPricePerMToken:  p.CacheRead,
			CacheWritePricePerMToken: p.CacheWrite,
		})
	}
	return nil
}

// SyncPricing fetches and merges a pricing document from each URL (e.g. a
// hosted price feed or a CDN). The client enforces a 10s timeout.
func SyncPricing(urls ...string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	for _, u := range urls {
		resp, err := client.Get(u)
		if err != nil {
			return fmt.Errorf("syncpricing: fetch %s: %w", u, err)
		}
		body, rerr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if rerr != nil {
			return fmt.Errorf("syncpricing: read %s: %w", u, rerr)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("syncpricing: %s returned %s", u, resp.Status)
		}
		if err := SyncPricingFromJSON(body); err != nil {
			return fmt.Errorf("syncpricing: %s: %w", u, err)
		}
	}
	return nil
}

func splitPricingKey(key string) (provider, model string, err error) {
	for i := 0; i < len(key); i++ {
		if key[i] == '/' {
			return key[:i], key[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("syncpricing: invalid pricing key %q (want \"provider/model\")", key)
}
