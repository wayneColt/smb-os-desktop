// Package money holds exact decimal types for amounts and hours, so that no
// figure an operator sees is the product of binary floating point.
package money

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// Cents is an amount of money in hundredths of the currency unit. It encodes
// to JSON as a number with exactly two decimals (412.50) and decodes only from
// numbers with at most two decimals, so a pack can never carry a fraction of a
// cent.
type Cents int64

// MarshalJSON writes c as a JSON number with two decimals.
func (c Cents) MarshalJSON() ([]byte, error) { return []byte(c.Decimal()), nil }

// UnmarshalJSON reads a JSON number with at most two decimals.
func (c *Cents) UnmarshalJSON(b []byte) error {
	v, err := parseFixed(b, 2)
	if err != nil {
		return fmt.Errorf("amount %s: %w", b, err)
	}
	*c = Cents(v)
	return nil
}

// Decimal formats c as "1234.56" (no grouping, no symbol).
func (c Cents) Decimal() string { return fixed(int64(c), 2) }

// String formats c for people: "$1,234.56" or "-$35.00".
func (c Cents) String() string {
	neg := c < 0
	v := int64(c)
	if neg {
		v = -v
	}
	whole := group(v / 100)
	s := fmt.Sprintf("$%s.%02d", whole, v%100)
	if neg {
		return "-" + s
	}
	return s
}

// Abs returns |c|.
func (c Cents) Abs() Cents {
	if c < 0 {
		return -c
	}
	return c
}

// MulRate returns c*rate rounded half away from zero to the cent. The rate is
// given in basis points of a basis point (1e-6), which is exact for any rate a
// franchise agreement writes down (6%, 2.5%, 1.75%).
func (c Cents) MulRate(ratePPM int64) Cents {
	p := int64(c) * ratePPM
	q, r := p/1_000_000, p%1_000_000
	if r < 0 {
		r = -r
	}
	if r*2 >= 1_000_000 {
		if p < 0 {
			q--
		} else {
			q++
		}
	}
	return Cents(q)
}

// RatePPM converts a fractional rate (0.06) to parts per million (60000).
// Rates in packs carry at most six decimals; validation enforces it.
func RatePPM(rate float64) int64 {
	if rate < 0 {
		return -RatePPM(-rate)
	}
	return int64(rate*1_000_000 + 0.5)
}

// Hours is a duration in hundredths of an hour. It encodes to JSON as a
// number with two decimals (46.20).
type Hours int64

// HoursFromMinutes converts whole minutes to hundredths of an hour, rounding
// half up.
func HoursFromMinutes(min int64) Hours {
	neg := min < 0
	if neg {
		min = -min
	}
	h := Hours((min*100 + 30) / 60)
	if neg {
		return -h
	}
	return h
}

// MarshalJSON writes h as a JSON number with two decimals.
func (h Hours) MarshalJSON() ([]byte, error) { return []byte(fixed(int64(h), 2)), nil }

// UnmarshalJSON reads a JSON number with at most two decimals.
func (h *Hours) UnmarshalJSON(b []byte) error {
	v, err := parseFixed(b, 2)
	if err != nil {
		return fmt.Errorf("hours %s: %w", b, err)
	}
	*h = Hours(v)
	return nil
}

// String formats h as "46.2" style text for sentences (trailing zero dropped).
func (h Hours) String() string {
	s := fixed(int64(h), 2)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

func fixed(v int64, places int) string {
	neg := v < 0
	if neg {
		v = -v
	}
	pow := int64(1)
	for i := 0; i < places; i++ {
		pow *= 10
	}
	s := fmt.Sprintf("%d.%0*d", v/pow, places, v%pow)
	if neg {
		return "-" + s
	}
	return s
}

func group(v int64) string {
	s := strconv.FormatInt(v, 10)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	pre := len(s) % 3
	if pre > 0 {
		out = append(out, s[:pre]...)
	}
	for i := pre; i < len(s); i += 3 {
		if len(out) > 0 {
			out = append(out, ',')
		}
		out = append(out, s[i:i+3]...)
	}
	return string(out)
}

// parseFixed parses a JSON number literal into an integer scaled by
// 10^places, refusing exponents and excess precision.
func parseFixed(b []byte, places int) (int64, error) {
	s := string(bytes.TrimSpace(b))
	if s == "" || s == "null" {
		return 0, fmt.Errorf("missing value")
	}
	if strings.ContainsAny(s, "eE") {
		return 0, fmt.Errorf("exponent form not allowed")
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" || len(frac) > places {
		return 0, fmt.Errorf("must have at most %d decimals", places)
	}
	for len(frac) < places {
		frac += "0"
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, err
	}
	f := int64(0)
	if frac != "" {
		if f, err = strconv.ParseInt(frac, 10, 64); err != nil {
			return 0, err
		}
	}
	pow := int64(1)
	for i := 0; i < places; i++ {
		pow *= 10
	}
	v := w*pow + f
	if neg {
		v = -v
	}
	return v, nil
}
