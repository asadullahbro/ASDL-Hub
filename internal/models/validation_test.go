package models

import "testing"

func TestParsePortMapping(t *testing.T) {
	cases := []struct {
		in              string
		host, container string
		ok              bool
	}{
		{"8080:8000", "8080", "8000", true},
		{"0.0.0.0:8080:8000/tcp", "8080", "8000", true},
		{"08080:8000", "8080", "8000", true},
		{"0000:8000", "", "", false}, // the value stored on the VPS today
		{"8000", "", "", false},
		{"70000:80", "", "", false},
		{"80:80; rm -rf /", "", "", false},
	}
	for _, tc := range cases {
		host, container, err := ParsePortMapping(tc.in)
		if (err == nil) != tc.ok || host != tc.host || container != tc.container {
			t.Errorf("ParsePortMapping(%q) = %q, %q, %v; want %q, %q, ok=%v", tc.in, host, container, err, tc.host, tc.container, tc.ok)
		}
	}
}

func TestProjectHostPort(t *testing.T) {
	if got := (&Project{}).HostPort(); got != "8000" {
		t.Errorf("no ports: HostPort() = %q, want 8000", got)
	}
	if got := (&Project{Ports: []string{"9000:8000"}}).HostPort(); got != "9000" {
		t.Errorf("HostPort() = %q, want 9000", got)
	}
}

func TestValidateProjectConfig(t *testing.T) {
	if err := ValidateProjectConfig("simplebanking-backend", "api.example.com", []string{"8080:8000"}, []EnvVar{{Key: "DATABASE_URL", Value: "x y 'z'"}}); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
	bad := []struct {
		name, domain string
		ports        []string
		env          []EnvVar
	}{
		{name: "bad name!"},
		{domain: "example.com; include /etc/passwd"},
		{ports: []string{"0000:8000"}},
		{env: []EnvVar{{Key: "BAD-KEY"}}},
	}
	for _, b := range bad {
		if err := ValidateProjectConfig(b.name, b.domain, b.ports, b.env); err == nil {
			t.Errorf("expected rejection for %+v", b)
		}
	}
}
