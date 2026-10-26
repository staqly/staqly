package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTokenPrices(t *testing.T) {
	p, err := LoadTokenPrices("")
	if err != nil {
		t.Fatal(err)
	}
	if got := tokenPrice(p, "gpt-6-luna"); got.Input != 0.1 || got.Output != 0.5 {
		t.Fatalf("gpt-6-luna %+v", got)
	}
	// A router's vendor/model id finds the bare one; an unknown model costs what the default does.
	if tokenPrice(p, "openai/gpt-6-astra").Output != 50 || tokenPrice(p, "someone/new-model") != p["google/gemini-2.5-flash"] {
		t.Fatal("lookup")
	}
	// 1M input of which half cached, 1M output: 0.5*2 + 0.5*0.1 + 10 = 11.05 USD.
	if got := tokenPrice(p, "gpt-6.1-sol").cost(1_000_000, 500_000, 1_000_000); got != 11_050_000 {
		t.Fatalf("cost %d", got)
	}

	// The operator's file adds models and replaces prices.
	f := filepath.Join(t.TempDir(), "prices.json")
	_ = os.WriteFile(f, []byte(`{"models":{"gpt-6-luna":{"input":1,"output":2},"my-local":{"input":0,"output":0}}}`), 0o600)
	p, err = LoadTokenPrices(f)
	if err != nil || p["gpt-6-luna"].Input != 1 || p["gpt-6-astra"].Input != 10 || tokenPrice(p, "my-local").cost(1e6, 0, 1e6) != 0 {
		t.Fatalf("file %v %+v", err, p)
	}
	_ = os.WriteFile(f, []byte(`{"models":{"x":{"input":-1}}}`), 0o600)
	if _, err := LoadTokenPrices(f); err == nil {
		t.Fatal("negative price")
	}
}
