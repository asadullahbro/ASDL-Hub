package db

import (
	"log"
	"os"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/asdl/hub/internal/models"
)

func Init(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(
		&models.Node{},
		&models.Heartbeat{},
		&models.Job{},
		&models.User{},
		&models.Session{},
		&models.Deployment{},
		&models.Container{},
		&models.Migration{},
		&models.Setting{},
		&models.PermanentToken{},
		&models.EnrollmentToken{},
		&models.WireGuardPeer{},
		&models.NodeSSHKey{},
	); err != nil {
		return nil, err
	}

	createDefaultAdmin(db)

	log.Println("✅ PostgreSQL database initialized successfully")
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
			adminPassword = "admin" // CHANGE THIS IN PRODUCTION!
			log.Println("⚠️  WARNING: Using default admin password 'admin'")
			log.Println("⚠️  Set ADMIN_PASSWORD environment variable to change it")
		}

		hashed, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("Failed to hash admin password: %v", err)
			return
		}

		admin := models.User{
			ID:       "admin",
			Username: "admin",
			Email:    "admin@asdlhub.com",
			Password: string(hashed),
			Role:     "admin",
		}

		if err := db.Create(&admin).Error; err != nil {
			log.Printf("Failed to create admin user: %v", err)
			return
		}

		log.Println("✅ Created default admin user")
		log.Println("   Username: admin")
		log.Printf("   Password: %s", adminPassword)
		log.Println("   ⚠️  Change this password immediately!")
	}
}
