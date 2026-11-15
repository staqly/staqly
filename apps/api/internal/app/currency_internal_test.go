package app

import "testing"

func TestCurrencyForCountry(t *testing.T) {
	for _, tc := range []struct{ country, want string }{
		{"CH", CurrencyCHF}, {"LI", CurrencyCHF},
		{"DE", CurrencyEUR}, {"FR", CurrencyEUR}, {"PL", CurrencyEUR}, {"SE", CurrencyEUR}, {"IE", CurrencyEUR}, {"CY", CurrencyEUR},
		{"AX", CurrencyEUR}, {"GF", CurrencyEUR}, {"GP", CurrencyEUR}, {"MQ", CurrencyEUR}, {"RE", CurrencyEUR}, {"YT", CurrencyEUR}, {"MF", CurrencyEUR},
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
		{[]string{"America/Cayenne"}, CurrencyEUR}, {[]string{"America/Marigot"}, CurrencyEUR}, {[]string{"Indian/Reunion"}, CurrencyEUR},
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
	pr := priceCacheEntry{currency: "eur", cents: 1500, options: map[string]int64{"chf": 1500, "usd": 1600}}
	for _, tc := range []struct {
		pr    priceCacheEntry
		cur   string
		cents int64
		sold  bool
	}{
		{pr, "chf", 1500, true},
		{pr, "usd", 1600, true},
		{pr, "eur", 1500, true}, // its own currency
		{pr, "gbp", 0, false},   // no such option: not sold there, never the price's own instead
		{priceCacheEntry{currency: "eur", cents: 1500}, "chf", 0, false}, // made before the options
	} {
		if cents, sold := tc.pr.in(tc.cur); cents != tc.cents || sold != tc.sold {
			t.Errorf("in(%s) = %d %v, want %d %v", tc.cur, cents, sold, tc.cents, tc.sold)
		}
	}
}
