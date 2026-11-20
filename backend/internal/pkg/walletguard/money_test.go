package walletguard

import (
	"math"
	"testing"
)

func TestExactAmount(t *testing.T) {
	for _, s := range []string{"0", "0.1", "0.10000001", "2", "1.23456789", "92233720368.54775807"} {
		a, err := ParseAmount(s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		b, err := ParseAmount(a.String())
		if err != nil || a != b {
			t.Fatalf("roundtrip %s", s)
		}
	}
	for _, s := range []string{"", "-1", "-0.00000001", "NaN", "Inf", "1e-8", " 1", "1.", ".1", "+1", "0.000000001", "92233720368.54775808", "999999999999999999999999999"} {
		if _, err := ParseAmount(s); err == nil {
			t.Fatalf("accepted invalid amount %q", s)
		}
	}
	if _, err := add(Amount(math.MaxInt64), 1); err == nil {
		t.Fatal("overflow accepted")
	}
}
func TestSplitBoundariesAndModes(t *testing.T) {
	tests := []struct {
		p, t    string
		member  bool
		mode, c string
		ok      bool
		wp, wt  string
	}{
		{"0", "0", false, "personal_only", "0.00000001", false, "", ""},
		{"0.1", "0", false, "personal_only", "0.01", false, "", ""},
		{"0.10000001", "0", false, "personal_only", "0.10000001", true, "0.10000001", "0"},
		{"1.99999999", "0", false, "personal_only", "1.99999999", true, "1.99999999", "0"},
		{"2", "0", false, "personal_only", "2", true, "2", "0"},
		{"0", "0.1", true, "team_only", "0.01", false, "", ""},
		{"0", "0.10000001", true, "team_only", "0.10000001", true, "0", "0.10000001"},
		{"1", "2", true, "team_first", "2.5", true, "0.5", "2"},
		{"1", "2", true, "personal_first", "2.5", true, "1", "1.5"},
		{"1", "2", true, "personal_only", "1.00000001", false, "", ""},
		{"2", "1", true, "team_only", "1.00000001", false, "", ""},
		{"2", "100", false, "team_first", "2.00000001", false, "", ""},
		{"2", "100", false, "team_only", "1", false, "", ""},
		{"0.05", "0.05", true, "team_first", "0.01", false, "", ""},
		{"0.05", "0.05000001", true, "personal_first", "0.1", true, "0.05", "0.05"},
	}
	for _, tc := range tests {
		t.Run(tc.mode+"/"+tc.p+"/"+tc.t+"/"+tc.c, func(t *testing.T) {
			p, _ := ParseAmount(tc.p)
			tm, _ := ParseAmount(tc.t)
			c, _ := ParseAmount(tc.c)
			a, b, err := split(p, tm, tc.member, tc.mode, c)
			if (err == nil) != tc.ok {
				t.Fatalf("got %v", err)
			}
			if tc.ok {
				wp, _ := ParseAmount(tc.wp)
				wt, _ := ParseAmount(tc.wt)
				if a != wp || b != wt {
					t.Fatalf("split %v %v", a, b)
				}
			}
		})
	}
}
