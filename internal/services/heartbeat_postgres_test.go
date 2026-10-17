package services

import (
	"fmt"
	"os"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/asdl/hub/internal/embeddedpg"
	"github.com/asdl/hub/internal/models"
)

// The trim uses VACUUM FULL, which only exists on PostgreSQL, so it can't be tested on the in-memory SQLite
// database. Starts a real PostgreSQL (downloads ~60 MB the first time): run with ASDL_TEST_EMBEDDED_PG=1.
func TestTrimHeartbeats_OnRealPostgres(t *testing.T) {
	if os.Getenv("ASDL_TEST_EMBEDDED_PG") != "1" {
		t.Skip("set ASDL_TEST_EMBEDDED_PG=1 to run against a real embedded PostgreSQL")
	}
	srv, err := embeddedpg.Start(embeddedpg.Config{Dir: t.TempDir(), Port: 54397, User: "asdl", Password: "testpw", Database: "asdl_hub"})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	db, err := gorm.Open(postgres.Open("host=127.0.0.1 port=54397 user=asdl password=testpw dbname=asdl_hub sslmode=disable"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Heartbeat{}); err != nil {
		t.Fatal(err)
	}

	rows := make([]models.Heartbeat, 0, 12000)
	for i := 0; i < 12000; i++ {
		rows = append(rows, models.Heartbeat{NodeID: fmt.Sprintf("n%d", i%3), CPUPercent: float64(i)})
	}
	if err := db.CreateInBatches(rows, 1000).Error; err != nil {
		t.Fatal(err)
	}

	if got := trimHeartbeats(db); got != 11997 {
		t.Fatalf("removed %d rows, want 11997", got)
	}
	var left []models.Heartbeat
	db.Order("node_id").Find(&left)
	if len(left) != 3 || left[0].CPUPercent != 11997 || left[1].CPUPercent != 11998 || left[2].CPUPercent != 11999 {
		t.Fatalf("did not keep each node's newest row: %+v", left)
	}
	// The table is usable after VACUUM FULL, and recordHeartbeat updates in place
	recordHeartbeat(db, &models.Heartbeat{NodeID: "n0", CPUPercent: 1})
	var n int64
	db.Model(&models.Heartbeat{}).Count(&n)
	if n != 3 {
		t.Fatalf("want 3 rows after an update in place, got %d", n)
	}
}
