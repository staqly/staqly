package app

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// TokenPrice is a language model's price in USD per million tokens.
type TokenPrice struct {
	Input       float64 `json:"input"`
	CachedInput float64 `json:"cached_input"` // 0: cached input costs what input does
	Output      float64 `json:"output"`
}

type tokenPrices struct {
	Default string                `json:"default"`
	Models  map[string]TokenPrice `json:"models"`
}

//go:embed ai_prices.json
var builtinTokenPrices []byte

// LoadTokenPrices reads the built-in price list, then the operator's file (AI_PRICES_FILE, same shape) over it.
func LoadTokenPrices(file string) (map[string]TokenPrice, error) {
	var base tokenPrices
	if err := json.Unmarshal(builtinTokenPrices, &base); err != nil {
		return nil, fmt.Errorf("built-in AI prices: %w", err)
	}
	base.Models[""] = base.Models[base.Default] // a model without a price of its own is counted at the default's
	if file == "" {
		return base.Models, nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("AI_PRICES_FILE: %w", err)
	}
	var extra tokenPrices
	if err := json.Unmarshal(b, &extra); err != nil {
		return nil, fmt.Errorf("AI_PRICES_FILE is not a price list like ai_prices.json: %w", err)
	}
	for id, p := range extra.Models {
		if p.Input < 0 || p.CachedInput < 0 || p.Output < 0 {
			return nil, fmt.Errorf("AI_PRICES_FILE: %s has a negative price", id)
		}
		base.Models[id] = p
	}
	return base.Models, nil
}

// tokenPrice is what a model's tokens cost: its own entry, the bare id of a router's "vendor/model", else the
// default's.
func tokenPrice(prices map[string]TokenPrice, model string) TokenPrice {
	if prices == nil { // a config made in code (tests): the built-in list
		prices = builtinPrices()
	}
	if p, ok := prices[model]; ok {
		return p
	}
	if _, bare, ok := strings.Cut(model, "/"); ok {
		if p, ok := prices[bare]; ok {
			return p
		}
	}
	return prices[""]
}

// cost in micro-USD of a call's tokens, cached input at its own price.
func (p TokenPrice) cost(in, cached, out int64) int64 {
	ci := p.CachedInput
	if ci == 0 {
		ci = p.Input
	}
	return int64(float64(in-cached)*p.Input + float64(cached)*ci + float64(out)*p.Output)
}

var builtinPrices = sync.OnceValue(func() map[string]TokenPrice {
	p, err := LoadTokenPrices("")
	if err != nil {
		panic(err) // the embedded file is checked by TestTokenPrices
	}
	return p
})
