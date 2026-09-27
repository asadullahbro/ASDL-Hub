package services

import (
	"bytes"
	"strings"
	"testing"
)

func renderRoutes(t *testing.T, routes []ContainerRoute) string {
	t.Helper()
	var buf bytes.Buffer
	err := nginxTemplate.Execute(&buf, NginxData{
		ACMEWebroot: "/var/www/asdl-acme",
		CertDir:     letsEncryptLiveDir,
		SSLOptions:  certbotSSLOptions,
		Routes:      routes,
	})
	if err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestNginxTemplate_TLSRouteUsesItsOwnCertificate(t *testing.T) {
	out := renderRoutes(t, []ContainerRoute{{Domain: "api.example.com", TLS: true, Locations: []RouteLocation{{Path: "/", NodeIP: "10.0.0.3", Port: "9000"}}}})
	for _, want := range []string{
		"ssl_certificate /etc/letsencrypt/live/api.example.com/fullchain.pem;",
		"return 301 https://$host$request_uri;",
		"proxy_pass http://10.0.0.3:9000;",
		"location /.well-known/acme-challenge/",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestNginxTemplate_NoCertServesHTTP(t *testing.T) {
	out := renderRoutes(t, []ContainerRoute{{Domain: "api.example.com", Locations: []RouteLocation{{Path: "/", NodeIP: "10.0.0.3", Port: "9000"}}}})
	if strings.Contains(out, "443") || strings.Contains(out, "return 301") {
		t.Errorf("route without a certificate must not listen on 443 or redirect:\n%s", out)
	}
	if !strings.Contains(out, "proxy_pass http://10.0.0.3:9000;") {
		t.Errorf("expected HTTP proxy:\n%s", out)
	}
}

func TestNginxTemplate_PathsShareADomainAndStripTheirPrefix(t *testing.T) {
	out := renderRoutes(t, []ContainerRoute{{Domain: "db.example.com", TLS: true, Locations: []RouteLocation{
		{Path: "/", NodeIP: "10.0.0.3", Port: "8000"},
		{Path: "/rest/v1/", NodeIP: "10.0.0.4", Port: "20002"},
	}}})
	for _, want := range []string{
		"location / {\n        proxy_pass http://10.0.0.3:8000;",
		"location /rest/v1/ {",
		"proxy_pass http://10.0.0.4:20002/;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "server_name db.example.com;"); n != 2 {
		t.Errorf("one domain = one HTTP and one HTTPS server block, got %d server_name lines", n)
	}
}
