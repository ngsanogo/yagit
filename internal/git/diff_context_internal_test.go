package git

import (
	"errors"
	"testing"
)

func TestUnifiedContextFlagDefaultsAndAllows(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   int
		want string
	}{
		{0, "-U3"},
		{3, "-U3"},
		{10, "-U10"},
		{25, "-U25"},
		{50, "-U50"},
	}
	for _, test := range cases {
		got, err := unifiedContextFlag(test.in)
		if err != nil {
			t.Fatalf("context %d: %v", test.in, err)
		}
		if got != test.want {
			t.Errorf("context %d: got %q, want %q", test.in, got, test.want)
		}
	}
}

func TestUnifiedContextFlagRefusesOthers(t *testing.T) {
	t.Parallel()

	for _, bad := range []int{-1, 1, 2, 4, 7, 100} {
		_, err := unifiedContextFlag(bad)
		if !errors.Is(err, ErrBadDiffContext) {
			t.Errorf("context %d: err = %v, want ErrBadDiffContext", bad, err)
		}
	}
}

func TestDiffSwitchesCarriesTheRequestedWidth(t *testing.T) {
	t.Parallel()

	switches, err := diffSwitches(10)
	if err != nil {
		t.Fatalf("diffSwitches: %v", err)
	}
	if switches[len(switches)-1] != "-U10" {
		t.Fatalf("last switch = %q, want -U10; full %v", switches[len(switches)-1], switches)
	}
	// The package-default list still ends in -U3 so show/stash stay put.
	if diffArgs[len(diffArgs)-1] != "-U3" {
		t.Fatalf("diffArgs last = %q, want -U3", diffArgs[len(diffArgs)-1])
	}
}
