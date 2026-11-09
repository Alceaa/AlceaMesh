package naming

import "testing"

func TestTunnelFromPath(t *testing.T) {
	cases := map[string]string{
		`C:\Users\Alcea\vps alceacore.conf`: "vps-alceacore",
		"/etc/wireguard/home.conf":          "home",
		"weird name!.conf":                  "weird-name-",
		"":                                  "AlceaMesh",
		".conf":                             "AlceaMesh",
	}
	for input, want := range cases {
		if got := TunnelFromPath(input); got != want {
			t.Errorf("TunnelFromPath(%q) = %q, want %q", input, got, want)
		}
	}
}
