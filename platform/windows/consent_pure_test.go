package windows

import "testing"

func TestUnmangleNonPackagedKey(t *testing.T) {
	cases := map[string]string{
		`C#Program Files#Teams#Teams.exe`: `C\Program Files\Teams\Teams.exe`,
		`plainname`:                       `plainname`,
		``:                                ``,
	}
	for in, want := range cases {
		if got := unmangleNonPackagedKey(in); got != want {
			t.Errorf("unmangleNonPackagedKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConsentEntryIsLive(t *testing.T) {
	cases := []struct {
		stop uint64
		want bool
	}{
		{stop: 0, want: true},
		{stop: 1, want: false},
		{stop: 132912345678900000, want: false},
	}
	for _, c := range cases {
		if got := consentEntryIsLive(c.stop); got != c.want {
			t.Errorf("consentEntryIsLive(%d) = %v, want %v", c.stop, got, c.want)
		}
	}
}
