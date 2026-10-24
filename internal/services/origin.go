package services

import (
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
)

var allowedOrigins atomic.Value // []string

// SetAllowedOrigins sets the browser origins, besides the Hub's own address,
// that may use a cookie session (the CORS allow-list).
func SetAllowedOrigins(origins []string) { allowedOrigins.Store(origins) }

// SameOrigin reports whether a request came from a page of the Hub itself (or
// an allow-listed origin). A request with no Origin is not from a browser page
// and passes: it can't ride on a cookie it has no way to send.
func SameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if list, ok := allowedOrigins.Load().([]string); ok {
		for _, a := range list {
			if origin == a {
				return true
			}
		}
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	for _, host := range []string{r.Host, r.Header.Get("X-Forwarded-Host")} {
		if host != "" && strings.EqualFold(u.Host, host) {
			return true
		}
	}
	return false
}
