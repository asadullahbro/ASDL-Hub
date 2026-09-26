// Package testutil provides a throwaway in-memory database for unit tests.
// Only ever imported from _test.go files, so it never ships in the binary.
package testutil

import (
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/asdl/hub/internal/models"
)

// NewDB opens a fresh in-memory SQLite database and migrates the given
// models. Each call gets its own isolated database — the DSN is given a
// unique name because SQLite's shared-cache in-memory mode (needed so
// gorm's connection pool sees the same schema across connections) would
// otherwise let same-named in-memory databases leak state between tests.
func NewDB(t *testing.T, dst ...interface{}) *gorm.DB {
	t.Helper()

	if err := models.SetSecretsKey("test-secrets-key"); err != nil {
		t.Fatalf("failed to set secrets key: %v", err)
	}

	dsn := fmt.Sprintf("file:%s_%d?mode=memory&cache=shared", t.Name(), time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}

	if len(dst) > 0 {
		if err := db.AutoMigrate(dst...); err != nil {
			t.Fatalf("failed to migrate: %v", err)
		}
	}

	return db
}
