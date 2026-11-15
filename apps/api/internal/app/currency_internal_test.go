package app

import "testing"

func TestCurrencyForCountry(t *testing.T) {
	for _, tc := range []struct{ country, want string }{
		{"CH", CurrencyCHF}, {"LI", CurrencyCHF},
		{"DE", CurrencyEUR}, {"FR", CurrencyEUR}, {"PL", CurrencyEUR}, {"SE", CurrencyEUR}, {"IE", CurrencyEUR}, {"CY", CurrencyEUR},
		{"GB", CurrencyUSD}, {"NO", CurrencyUSD}, {"US", CurrencyUSD}, {"JP", CurrencyUSD}, {"", CurrencyUSD}, {"XX", CurrencyUSD},
	} {
		if got := currencyForCountry(tc.country); got != tc.want {
			t.Errorf("currencyForCountry(%q) = %s, want %s", tc.country, got, tc.want)
		}
	}
}

func TestCurrencyForZones(t *testing.T) {
	for _, tc := range []struct {
		zones []string
		want  string
	}{
		{[]string{"Europe/Zurich"}, CurrencyCHF},
		{[]string{"Europe/Vaduz"}, CurrencyCHF},
		{[]string{"Europe/Berlin"}, CurrencyEUR},
		{[]string{"Europe/Busingen"}, CurrencyEUR}, // German, on Zurich time
		{[]string{"Atlantic/Canary"}, CurrencyEUR},
		{[]string{"Europe/Warsaw"}, CurrencyEUR},
		{[]string{"Europe/London"}, CurrencyUSD},
		{[]string{"Europe/Oslo"}, CurrencyUSD},
		{[]string{"America/New_York"}, CurrencyUSD},
		{[]string{"Asia/Tokyo"}, CurrencyUSD},
		{nil, CurrencyUSD},
		{[]string{""}, CurrencyUSD},
		// UTC (a workspace's default) says nothing: the next zone decides.
		{[]string{"UTC", "Europe/Zurich"}, CurrencyCHF},
		{[]string{"Etc/UTC", "Europe/Paris"}, CurrencyEUR},
		{[]string{"UTC", ""}, CurrencyUSD},
		// A zone that says where someone is wins over the next one.
		{[]string{"America/Chicago", "Europe/Zurich"}, CurrencyUSD},
	} {
		if got := currencyForZones(tc.zones...); got != tc.want {
			t.Errorf("currencyForZones(%q) = %s, want %s", tc.zones, got, tc.want)
		}
	}
}

func TestPriceIn(t *testing.T) {
	pr := priceCacheEntry{currency: "eur", cents: 1500, options: map[string]int64{"eur": 1500, "chf": 1500, "usd": 1600}}
	for _, tc := range []struct {
		pr        priceCacheEntry
		want, cur string
		cents     int64
	}{
		{pr, "chf", "chf", 1500},
		{pr, "usd", "usd", 1600},
		{pr, "eur", "eur", 1500},
		{pr, "gbp", "eur", 1500}, // no such option: the price's own
		{priceCacheEntry{currency: "eur", cents: 1500}, "chf", "eur", 1500}, // made before the options
	} {
		if cur, cents := tc.pr.in(tc.want); cur != tc.cur || cents != tc.cents {
			t.Errorf("in(%s) = %s %d, want %s %d", tc.want, cur, cents, tc.cur, tc.cents)
		}
	}
}
