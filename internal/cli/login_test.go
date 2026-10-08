package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// A user with two-factor on gets no token from the password step; login must
// ask for the code and save the session it earns.
func TestLogin_AsksForTheTwoFactorCode(t *testing.T) {
	var gotCode string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			json.NewEncoder(w).Encode(map[string]any{"mfa_required": true, "mfa_token": "step-two"})
		case "/api/v1/auth/2fa/login":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			gotCode = body["code"]
			if body["mfa_token"] != "step-two" || body["code"] != "123456" {
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "that code is wrong or expired"})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"token": "session-token", "user": map[string]string{"username": "alice", "role": "viewer"}})
		case "/api/v1/auth/me":
			if r.Header.Get("Authorization") != "Bearer session-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"username": "alice", "role": "viewer"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	run := func(stdin string) error {
		rd, wr, _ := os.Pipe()
		wr.WriteString(stdin)
		wr.Close()
		old := os.Stdin
		os.Stdin = rd
		defer func() { os.Stdin = old }()
		var out bytes.Buffer
		return (&env{out: &out}).login([]string{srv.URL, "--user", "alice"})
	}

	if err := run("a-password\n000000\n"); err == nil || !strings.Contains(err.Error(), "wrong or expired code") {
		t.Errorf("a wrong code: got %v", err)
	}
	if err := run("a-password\n123456\n"); err != nil {
		t.Fatalf("right code: %v", err)
	}
	if gotCode != "123456" {
		t.Errorf("the code sent was %q", gotCode)
	}
	saved, err := os.ReadFile(loginPath())
	if err != nil || !strings.Contains(string(saved), "session-token") {
		t.Errorf("the session wasn't saved: %v %s", err, saved)
	}
}
