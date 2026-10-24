package db

import (
	"log"
	"os"
	"strconv"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/asdl/hub/internal/models"
)

// ProductionMode is PRODUCTION_MODE in the Hub's .env, true unless set to
// false: quiet logs for a live Hub. Off, the Hub logs every database query
// and the web server's debug output, for chasing a problem.
func ProductionMode() bool {
	return os.Getenv("PRODUCTION_MODE") != "false"
}

// maxConns is how many database connections the Hub may hold open: DB_MAX_CONNS, 40 by default.
func maxConns() int {
	if n, err := strconv.Atoi(os.Getenv("DB_MAX_CONNS")); err == nil && n > 0 {
		return n
	}
	return 40
}

func Init(dsn string) (*gorm.DB, error) {
	// Logging every statement floods the journal (nodes poll every few
	// seconds) and would print stored values, so only warnings and errors are
	// logged in production mode (or unless LOG_LEVEL=debug, the older switch).
	level := logger.Warn
	if !ProductionMode() || os.Getenv("LOG_LEVEL") == "debug" {
		level = logger.Info
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.New(log.New(os.Stdout, "", log.LstdFlags), logger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  level,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		}),
	})
	if err != nil {
		return nil, err
	}

	// PostgreSQL allows 100 connections (the embedded one is set to that) and the Hub is not its only user,
	// so the pool is capped. Without a cap, a burst of queries (the health checker looks up every app's node
	// at once) opened a connection each, ran past the limit, and made the agents' own requests fail with
	// "node not found". Queries over the cap wait for a free connection instead.
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(maxConns())
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetConnMaxLifetime(30 * time.Minute)
	}

	// Older versions made repository unique even when empty, so only one
	// project without a repository could exist.
	// (gorm's Migrator().DropIndex emits invalid SQL on PostgreSQL 12.)
	if err := db.Exec("DROP INDEX IF EXISTS idx_projects_repository").Error; err != nil {
		log.Printf("⚠️ Could not drop old projects.repository index: %v", err)
	}

	if err := db.AutoMigrate(
		&models.Node{},
		&models.Heartbeat{},
		&models.NodeOutage{},
		&models.NodeSample{},
		&models.AppOutage{},
		&models.Job{},
		&models.User{},
		&models.Session{},
		&models.Passkey{},
		&models.Deployment{},
		&models.Container{},
		&models.Migration{},
		&models.Setting{},
		&models.PermanentToken{},
		&models.EnrollmentToken{},
		&models.WireGuardPeer{},
		&models.NodeSSHKey{},
		&models.Project{},
		&models.ProjectPlugin{},
		&models.NotificationChannel{},
		&models.AllowedRepo{},
		&models.GitHubToken{},
		&models.OIDCDeployment{},
	); err != nil {
		return nil, err
	}
	createDefaultAdmin(db)

	log.Println("PostgreSQL database initialized successfully")
	return db, nil
}

func InitPostgres(dsn string) (*gorm.DB, error) {
	return Init(dsn)
}

func createDefaultAdmin(db *gorm.DB) {
	var count int64
	db.Model(&models.User{}).Count(&count)

	if count == 0 {
		// Get admin password from env or use default
		adminPassword := os.Getenv("ADMIN_PASSWORD")
		if adminPassword == "" {
			adminPassword = "admin" // CHANGE THIS IN PRODUCTION
			log.Println("WARNING: Using default admin password 'admin'")
			log.Println("Set ADMIN_PASSWORD environment variable to change it")
		}

		hashed, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("Failed to hash admin password: %v", err)
			return
		}

		admin := models.User{
			ID:       "admin",
			Username: "admin",
			Email:    "admin@admin.go",
			Password: string(hashed),
			Role:     "admin",
		}

		if err := db.Create(&admin).Error; err != nil {
			log.Printf("Failed to create admin user: %v", err)
			return
		}

		log.Println("Created default admin user")
		log.Println("Username: admin")
		log.Printf("Password: %s", adminPassword)
		log.Println("Change this password immediately!")
	}
}
