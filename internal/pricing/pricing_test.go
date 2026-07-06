package pricing

import "testing"

func almostEqual(a, b float64) bool {
	d := a - b
	return d < 1e-9 && d > -1e-9
}

func TestCostOpus(t *testing.T) {
	// 1M each: input $5 + output $25 + cache_read 0.1*5=$0.5 + cc5m 1.25*5=$6.25 + cc1h 2.0*5=$10
	c, ok := Cost("claude-opus-4-8", 1_000_000, 1_000_000, 1_000_000, 1_000_000, 1_000_000)
	if !ok {
		t.Fatal("expected priced")
	}
	want := 5.0 + 25.0 + 0.5 + 6.25 + 10.0
	if !almostEqual(c, want) {
		t.Fatalf("cost = %v, want %v", c, want)
	}
}

func TestCostCacheTTLSplit(t *testing.T) {
	// 1h writes cost more than 5m writes for the same token count.
	c5, _ := Cost("claude-opus-4-8", 0, 0, 0, 1_000_000, 0)
	c1h, _ := Cost("claude-opus-4-8", 0, 0, 0, 0, 1_000_000)
	if !almostEqual(c5, 6.25) || !almostEqual(c1h, 10.0) {
		t.Fatalf("cc5m=%v (want 6.25), cc1h=%v (want 10)", c5, c1h)
	}
}

func TestCostUnknownModelUnpriced(t *testing.T) {
	if c, ok := Cost("gpt-4o", 100, 100, 0, 0, 0); ok || c != 0 {
		t.Fatalf("expected unpriced (0,false) for unknown model, got (%v,%v)", c, ok)
	}
}

func TestFamily(t *testing.T) {
	cases := map[string]string{
		"claude-opus-4-8":         "opus",
		"claude-sonnet-5":         "sonnet",
		"claude-haiku-4-5-2025":   "haiku",
		"claude-3-5-haiku-latest": "haiku",
		"gpt-4o":                  "",
		"":                        "",
	}
	for in, want := range cases {
		if got := Family(in); got != want {
			t.Errorf("Family(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRatesIsCopy(t *testing.T) {
	r := Rates()
	r["opus"] = ModelRate{Input: 999, Output: 999}
	if Rates()["opus"].Input == 999 {
		t.Fatal("Rates() must return a copy, not the shared table")
	}
}
