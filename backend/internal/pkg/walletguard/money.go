// Package walletguard provides a transactional wallet reservation primitive.
// It is NOT yet wired to gateway admission or usage settlement. See the rollout
// document before using it; an estimate is not a proven charge ceiling.
package walletguard

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Amount is an exact number of 10^-8 balance units, never a float64.
// Values outside this range are rejected even though PostgreSQL numeric(20,8)
// can represent more. This restriction is fail-closed, not saturation.
type Amount int64

const Floor Amount = 10_000_000 // 0.1; admission must be strictly greater.

var ErrAmount = errors.New("walletguard: invalid or unsupported exact amount")

// ParseAmount accepts nonnegative ordinary decimal notation with at most eight
// fractional digits. It intentionally rejects floats, exponents and rounding.
func ParseAmount(s string) (Amount, error) {
	if s == "" || strings.TrimSpace(s) != s {
		return 0, ErrAmount
	}
	parts := strings.Split(s, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, ErrAmount
	}
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
		if frac == "" || len(frac) > 8 {
			return 0, ErrAmount
		}
	}
	for _, r := range parts[0] + frac {
		if r < '0' || r > '9' {
			return 0, ErrAmount
		}
	}
	whole, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || whole > math.MaxInt64/100_000_000 {
		return 0, ErrAmount
	}
	frac += strings.Repeat("0", 8-len(frac))
	f, err := strconv.ParseUint(frac, 10, 64)
	if err != nil || whole*100_000_000 > math.MaxInt64-f {
		return 0, ErrAmount
	}
	return Amount(whole*100_000_000 + f), nil
}

func (a Amount) String() string {
	if a < 0 {
		return "invalid-negative-amount"
	}
	return fmt.Sprintf("%d.%08d", int64(a)/100_000_000, int64(a)%100_000_000)
}

func add(a, b Amount) (Amount, error) {
	if a < 0 || b < 0 || a > Amount(math.MaxInt64)-b {
		return 0, ErrAmount
	}
	return a + b, nil
}

func min(a, b Amount) Amount {
	if a < b {
		return a
	}
	return b
}
