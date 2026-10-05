package money

import (
	"encoding/json"
	"testing"
)

func TestCentsRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Cents
		out  string
	}{
		{"412.50", 41250, "412.50"},
		{"412.5", 41250, "412.50"},
		{"7", 700, "7.00"},
		{"-35.00", -3500, "-35.00"},
		{"0.07", 7, "0.07"},
	} {
		var c Cents
		if err := json.Unmarshal([]byte(tc.in), &c); err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if c != tc.want {
			t.Fatalf("%s: got %d want %d", tc.in, c, tc.want)
		}
		b, _ := json.Marshal(c)
		if string(b) != tc.out {
			t.Fatalf("%s: marshaled %s want %s", tc.in, b, tc.out)
		}
	}
}

func TestCentsRefusesFractionsOfACent(t *testing.T) {
	for _, in := range []string{"1.005", "1e3", `"12.00"`, "null"} {
		var c Cents
		if err := json.Unmarshal([]byte(in), &c); err == nil {
			t.Fatalf("%s should be refused", in)
		}
	}
}

func TestCentsString(t *testing.T) {
	if got := Cents(1490064).String(); got != "$14,900.64" {
		t.Fatal(got)
	}
	if got := Cents(-3500).String(); got != "-$35.00" {
		t.Fatal(got)
	}
	if got := Cents(5).String(); got != "$0.05" {
		t.Fatal(got)
	}
}

func TestMulRateRoundsHalfAwayFromZero(t *testing.T) {
	// 71,842.60 × 6% = 4,310.556 → 4,310.56
	if got := Cents(7184260).MulRate(RatePPM(0.06)); got != 431056 {
		t.Fatalf("got %d", got)
	}
	// 0.25 × 2% = 0.005 → 0.01
	if got := Cents(25).MulRate(RatePPM(0.02)); got != 1 {
		t.Fatalf("got %d", got)
	}
	if got := Cents(-25).MulRate(RatePPM(0.02)); got != -1 {
		t.Fatalf("got %d", got)
	}
	if RatePPM(0.015) != 15000 {
		t.Fatal(RatePPM(0.015))
	}
}

func TestHoursFromMinutes(t *testing.T) {
	if got := HoursFromMinutes(2772); got != 4620 {
		t.Fatalf("2772 min = %d", got)
	}
	if got := HoursFromMinutes(1); got != 2 { // 1/60 h = 0.0167 → 0.02
		t.Fatalf("1 min = %d", got)
	}
	b, _ := json.Marshal(Hours(4620))
	if string(b) != "46.20" || Hours(4620).String() != "46.2" || Hours(800).String() != "8" {
		t.Fatalf("formatting: %s %s %s", b, Hours(4620).String(), Hours(800).String())
	}
}
