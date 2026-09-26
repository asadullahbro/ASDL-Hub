package services

import "testing"

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.6.2", "v0.6.1", true},
		{"v0.10.0", "v0.9.9", true},
		{"v1.0.0", "v0.99.99", true},
		{"v0.6.1", "v0.6.1", false},
		{"v0.6.0", "v0.6.1", false},
		{"v0.6.2", "dev", false},
		{"", "v0.6.1", false},
		{"v0.6.2-rc1", "v0.6.1", false},
	}
	for _, tc := range cases {
		if got := newerVersion(tc.a, tc.b); got != tc.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
