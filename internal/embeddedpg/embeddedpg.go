// Package embeddedpg runs the Hub's PostgreSQL as a child process of the Hub, so one service (and one
// systemctl command) covers both. The PostgreSQL binaries are downloaded once on first start and cached.
// It is opt-in (DB_EMBEDDED=true): without it the Hub connects to a PostgreSQL you run yourself.
package embeddedpg

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	epg "github.com/fergusstrange/embedded-postgres"
)

// DefaultPort is deliberately not 5432, so the embedded server never clashes with a PostgreSQL already
// installed on the machine (which matters while moving an existing Hub over).
const DefaultPort = 54329

// Config is how to run the embedded server. Everything lives under Dir.
type Config struct {
	Dir      string // data, cached binaries and runtime files
	Port     uint32
	User     string
	Password string
	Database string
}

// Enabled reports whether the Hub should run its own PostgreSQL (DB_EMBEDDED=true).
func Enabled() bool { return os.Getenv("DB_EMBEDDED") == "true" }

// ConfigFromEnv reads the embedded server's settings. workDir is the Hub's folder; the data goes in
// workDir/data/postgres unless DB_DATA_DIR says otherwise.
func ConfigFromEnv(workDir string) Config {
	c := Config{
		Dir:      filepath.Join(workDir, "data", "postgres"),
		Port:     DefaultPort,
		User:     envOr("DB_USER", "asdl"),
		Password: os.Getenv("DB_PASSWORD"),
		Database: envOr("DB_NAME", "asdl_hub"),
	}
	if v := os.Getenv("DB_DATA_DIR"); v != "" {
		c.Dir = v
	}
	if v, err := strconv.ParseUint(os.Getenv("DB_PORT"), 10, 32); err == nil && v > 0 && v < 65536 {
		c.Port = uint32(v)
	}
	return c
}

// Validate refuses settings that would leave the database open or unusable.
func (c Config) Validate() error {
	switch {
	case c.Dir == "":
		return fmt.Errorf("no data directory")
	case c.Password == "" || c.Password == "dbpass123":
		return fmt.Errorf("set DB_PASSWORD to a real password before running the embedded database")
	case strings.ContainsAny(c.User+c.Database, " '\"\\;"):
		return fmt.Errorf("DB_USER and DB_NAME may not contain spaces or quotes")
	}
	return nil
}

// Server is a running embedded PostgreSQL.
type Server struct {
	pg   *epg.EmbeddedPostgres
	once sync.Once
	err  error
}

// Start initialises the data directory on first use, starts PostgreSQL on 127.0.0.1 and returns once it
// accepts connections. The first start downloads the binaries, so it can take a while.
func Start(c Config) (*Server, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	for _, sub := range []string{"data", "bin", "run"} {
		if err := os.MkdirAll(filepath.Join(c.Dir, sub), 0o700); err != nil {
			return nil, fmt.Errorf("create %s: %w", filepath.Join(c.Dir, sub), err)
		}
	}
	cfg := epg.DefaultConfig().
		Version(epg.V16).
		Port(c.Port).
		Username(c.User).
		Password(c.Password).
		Database(c.Database).
		DataPath(filepath.Join(c.Dir, "data")).
		BinariesPath(filepath.Join(c.Dir, "bin")).
		RuntimePath(filepath.Join(c.Dir, "run")).
		Logger(&lineFilter{w: os.Stdout}).
		StartParameters(map[string]string{
			// Small, because the Hub often runs on a 1 GB machine
			"shared_buffers":   "32MB",
			"listen_addresses": "127.0.0.1",
			"max_connections":  "100",
		})
	pg := epg.NewDatabase(cfg)
	log.Printf("🐘 Starting the embedded PostgreSQL on 127.0.0.1:%d (data in %s)", c.Port, c.Dir)
	if err := pg.Start(); err != nil {
		return nil, fmt.Errorf("start embedded PostgreSQL: %w", err)
	}
	return &Server{pg: pg}, nil
}

// Stop shuts PostgreSQL down cleanly. Safe to call more than once.
func (s *Server) Stop() error {
	if s == nil {
		return nil
	}
	s.once.Do(func() { s.err = s.pg.Stop() })
	return s.err
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// lineFilter passes only PostgreSQL's problems to the log, not its routine messages (every checkpoint).
type lineFilter struct {
	w   io.Writer
	buf bytes.Buffer
}

func (f *lineFilter) Write(p []byte) (int, error) {
	f.buf.Write(p)
	for {
		line, err := f.buf.ReadString('\n')
		if err != nil {
			f.buf.WriteString(line) // keep the partial line
			break
		}
		if keep(line) {
			fmt.Fprintf(f.w, "🐘 %s", line)
		}
	}
	return len(p), nil
}

func keep(line string) bool {
	for _, level := range []string{"ERROR", "FATAL", "PANIC", "WARNING"} {
		if strings.Contains(line, level+":") {
			return true
		}
	}
	return false
}
