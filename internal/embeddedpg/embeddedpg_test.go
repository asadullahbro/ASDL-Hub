package embeddedpg

import (
	"bytes"
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/lib/pq"
)

func TestConfigFromEnvDefaults(t *testing.T) {
	for _, k := range []string{"DB_DATA_DIR", "DB_PORT", "DB_USER", "DB_NAME"} {
		t.Setenv(k, "")
	}
	t.Setenv("DB_PASSWORD", "s3cret")
	c := ConfigFromEnv("/opt/asdl-hub")
	if c.Dir != "/opt/asdl-hub/data/postgres" || c.Port != DefaultPort || c.User != "asdl" || c.Database != "asdl_hub" || c.Password != "s3cret" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("defaults with a password should be valid: %v", err)
	}
}

func TestConfigFromEnvOverrides(t *testing.T) {
	t.Setenv("DB_DATA_DIR", "/srv/pg")
	t.Setenv("DB_PORT", "6000")
	t.Setenv("DB_USER", "hub")
	t.Setenv("DB_NAME", "hubdb")
	c := ConfigFromEnv("/opt/asdl-hub")
	if c.Dir != "/srv/pg" || c.Port != 6000 || c.User != "hub" || c.Database != "hubdb" {
		t.Fatalf("overrides ignored: %+v", c)
	}
	t.Setenv("DB_PORT", "99999")
	if ConfigFromEnv("/x").Port != DefaultPort {
		t.Fatal("an out-of-range port must fall back to the default")
	}
}

func TestValidateRefusesUnsafeSettings(t *testing.T) {
	good := Config{Dir: "/d", Port: 1, User: "asdl", Password: "pw", Database: "db"}
	for name, mutate := range map[string]func(*Config){
		"no password":      func(c *Config) { c.Password = "" },
		"default password": func(c *Config) { c.Password = "dbpass123" },
		"quote in name":    func(c *Config) { c.Database = "a'b" },
		"no dir":           func(c *Config) { c.Dir = "" },
	} {
		c := good
		mutate(&c)
		if c.Validate() == nil {
			t.Errorf("%s should be refused", name)
		}
	}
}

func TestLineFilterKeepsOnlyProblems(t *testing.T) {
	var out bytes.Buffer
	f := &lineFilter{w: &out}
	f.Write([]byte("2026 LOG:  checkpoint starting\n2026 FATAL:  password authentication failed\n2026 WARN"))
	f.Write([]byte("ING:  something odd\n2026 LOG:  ready\n"))
	got := out.String()
	if strings.Contains(got, "checkpoint") || strings.Contains(got, "ready") {
		t.Fatalf("routine lines leaked: %q", got)
	}
	if !strings.Contains(got, "FATAL:") || !strings.Contains(got, "WARNING:") {
		t.Fatalf("problems were dropped (including one split across writes): %q", got)
	}
}

// Starts a real PostgreSQL (downloads ~60 MB the first time): run with ASDL_TEST_EMBEDDED_PG=1.
func TestStartAndConnect(t *testing.T) {
	if os.Getenv("ASDL_TEST_EMBEDDED_PG") != "1" {
		t.Skip("set ASDL_TEST_EMBEDDED_PG=1 to start a real embedded PostgreSQL")
	}
	c := Config{Dir: t.TempDir(), Port: 54398, User: "asdl", Password: "testpw", Database: "asdl_hub"}
	s, err := Start(c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	db, err := sql.Open("postgres", "host=127.0.0.1 port=54398 user=asdl password=testpw dbname=asdl_hub sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("select 1").Scan(&n); err != nil || n != 1 {
		t.Fatalf("query failed: %v (n=%d)", err, n)
	}
	// Wrong password must be refused: the server is not open to anyone on the machine
	bad, _ := sql.Open("postgres", "host=127.0.0.1 port=54398 user=asdl password=wrong dbname=asdl_hub sslmode=disable")
	defer bad.Close()
	if bad.QueryRow("select 1").Scan(&n) == nil {
		t.Fatal("a wrong password was accepted")
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if s.Stop() != nil {
		t.Fatal("a second Stop must be harmless")
	}
}
