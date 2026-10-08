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

// A hash to compare against when the username doesn't exist, so an unknown
// user takes as long to refuse as a wrong password and the response time
// doesn't reveal which usernames exist.
var unknownUserHash, _ = bcrypt.GenerateFromPassword([]byte("no-such-user"), bcrypt.DefaultCost)

func (s *AuthService) Login(username, password string) (*models.User, string, error) {
	var user models.User
	if err := s.db.Where("username = ?", username).First(&user).Error; err != nil {
		_ = bcrypt.CompareHashAndPassword(unknownUserHash, []byte(password))
		return nil, "", errors.New("invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil, "", errors.New("invalid credentials")
	}

	if user.TOTPEnabled {
		return &user, "", ErrTwoFactorRequired
	}
	tokenString, err := s.sessionToken(&user)
	if err != nil {
		return nil, "", err
	}
	return &user, tokenString, nil
}

// sessionToken is the token a signed-in user carries.
func (s *AuthService) sessionToken(user *models.User) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID,
		"role":    user.Role,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
	}).SignedString([]byte(s.jwtSecret))
}

func (s *AuthService) ValidateToken(tokenString string) (*models.User, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return []byte(s.jwtSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))

	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid claims")
	}

	// A token from the password step of a two-factor sign-in is not a session.
	if tokenType, _ := claims["type"].(string); tokenType == "mfa" {
		return nil, errors.New("invalid token")
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
