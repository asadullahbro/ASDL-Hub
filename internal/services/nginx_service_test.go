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
	out := renderRoutes(t, []ContainerRoute{{Domain: "api.example.com", NodeIP: "10.0.0.3", Port: "9000", TLS: true}})
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
	out := renderRoutes(t, []ContainerRoute{{Domain: "api.example.com", NodeIP: "10.0.0.3", Port: "9000"}})
	if strings.Contains(out, "443") || strings.Contains(out, "return 301") {
		t.Errorf("route without a certificate must not listen on 443 or redirect:\n%s", out)
	}
	if !strings.Contains(out, "proxy_pass http://10.0.0.3:9000;") {
		t.Errorf("expected HTTP proxy:\n%s", out)
	}
}
