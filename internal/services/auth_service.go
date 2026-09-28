// internal/services/auth_service.go
package services

import (
	"errors"
	"log"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

type AuthService struct {
	db        *gorm.DB
	jwtSecret string
}

func (s *AuthService) GetJWTSecret() string {
	return s.jwtSecret
}

func NewAuthService(db *gorm.DB, jwtSecret string) *AuthService {
	return &AuthService{
		db:        db,
		jwtSecret: jwtSecret,
	}
}

func (s *AuthService) Login(username, password string) (*models.User, string, error) {
	var user models.User
	if err := s.db.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, "", errors.New("invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil, "", errors.New("invalid credentials")
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID,
		"role":    user.Role,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
	})

	tokenString, err := token.SignedString([]byte(s.jwtSecret))
	if err != nil {
		return nil, "", err
	}

	return &user, tokenString, nil
}

func (s *AuthService) ValidateToken(tokenString string) (*models.User, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return []byte(s.jwtSecret), nil
	})

	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid claims")
	}

	// Handle permanent tokens — look up by token value
	if tokenType, ok := claims["type"].(string); ok && tokenType == "permanent" {
		var pt models.PermanentToken
		if err := s.db.Where("token = ?", tokenString).First(&pt).Error; err != nil {
			return nil, errors.New("permanent token not found or revoked")
		}
		// Return the user who created it
		var user models.User
		if err := s.db.First(&user, "id = ?", pt.CreatedBy).Error; err != nil {
			return nil, errors.New("token owner not found")
		}
		return &user, nil
	}

	// Regular JWT
	userID, ok := claims["user_id"].(string)
	if !ok {
		return nil, errors.New("invalid user_id")
	}

	var user models.User
	if err := s.db.First(&user, "id = ?", userID).Error; err != nil {
		return nil, errors.New("user not found")
	}

	return &user, nil
}

// StartCLIToken keeps an admin token for the local command line in path
// (relative to the working directory, readable only by the Hub's user, so
// root on this server can run asdl-hub commands without logging in). It is
// renewed daily and valid for a week.
func (s *AuthService) StartCLIToken(path string) {
	write := func() {
		var admin models.User
		if err := s.db.Where("role = ?", models.RoleAdmin).Order("created_at").First(&admin).Error; err != nil {
			return
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"user_id": admin.ID,
			"role":    admin.Role,
			"exp":     time.Now().Add(7 * 24 * time.Hour).Unix(),
		})
		signed, err := token.SignedString([]byte(s.jwtSecret))
		if err != nil {
			return
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(signed+"\n"), 0o600); err != nil {
			log.Printf("⚠️ Could not write the command line's token: %v", err)
			return
		}
		_ = os.Rename(tmp, path)
	}
	go func() {
		for {
			write()
			time.Sleep(24 * time.Hour)
		}
	}()
}
