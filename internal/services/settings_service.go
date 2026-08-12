package services

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

type SettingsService struct {
	db          *gorm.DB
	authService *AuthService
	jwtSecret   string
}

func NewSettingsService(db *gorm.DB, authService *AuthService, jwtSecret string) *SettingsService {
	return &SettingsService{db: db, authService: authService, jwtSecret: jwtSecret}
}

// --- Sudo verification ---

func (s *SettingsService) VerifyAdminPassword(userID, password string) error {
	var user models.User
	if err := s.db.First(&user, "id = ?", userID).Error; err != nil {
		return errors.New("user not found")
	}
	if user.Role != models.RoleAdmin {
		return errors.New("admin access required")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return errors.New("incorrect password")
	}
	return nil
}

// --- Permanent tokens ---

func (s *SettingsService) GeneratePermanentToken(name, createdBy string) (string, *models.PermanentToken, error) {
	claims := jwt.MapClaims{
		"token_id": uuid.New().String(),
		"type":     "permanent",
		"exp":      time.Now().Add(10 * 365 * 24 * time.Hour).Unix(), // 10 years
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(s.jwtSecret))
	if err != nil {
		return "", nil, err
	}

	hint := tokenString[len(tokenString)-8:]
	pt := &models.PermanentToken{
		ID:        uuid.New().String(),
		Name:      name,
		Token:     tokenString,
		TokenHint: "..." + hint,
		CreatedBy: createdBy,
		CreatedAt: time.Now(),
	}
	if err := s.db.Create(pt).Error; err != nil {
		return "", nil, err
	}
	return tokenString, pt, nil
}

func (s *SettingsService) ListPermanentTokens() ([]models.PermanentToken, error) {
	var tokens []models.PermanentToken
	if err := s.db.Order("created_at DESC").Find(&tokens).Error; err != nil {
		return nil, err
	}
	return tokens, nil
}

func (s *SettingsService) RevokePermanentToken(id string) error {
	return s.db.Delete(&models.PermanentToken{}, "id = ?", id).Error
}

// --- GitHub token ---

func (s *SettingsService) SetGitHubToken(token string) error {
	setting := models.Setting{
		Key:       "github_token",
		Value:     token,
		UpdatedAt: time.Now(),
	}
	return s.db.Save(&setting).Error
}

func (s *SettingsService) GetGitHubToken() (string, error) {
	var setting models.Setting
	if err := s.db.First(&setting, "key = ?", "github_token").Error; err != nil {
		return "", nil // not set yet, not an error
	}
	return setting.Value, nil
}

func (s *SettingsService) GetGitHubTokenMasked() string {
	token, err := s.GetGitHubToken()
	if err != nil || token == "" {
		return ""
	}
	if len(token) <= 8 {
		return "••••••••"
	}
	return "••••••••" + token[len(token)-4:]
}

// --- Master node ---

func (s *SettingsService) SetMasterNode(nodeID string) error {
	// Verify node exists
	var node models.Node
	if err := s.db.First(&node, "id = ?", nodeID).Error; err != nil {
		return errors.New("node not found")
	}
	setting := models.Setting{
		Key:       "master_node_id",
		Value:     nodeID,
		UpdatedAt: time.Now(),
	}
	return s.db.Save(&setting).Error
}

func (s *SettingsService) GetMasterNodeID() string {
	var setting models.Setting
	if err := s.db.First(&setting, "key = ?", "master_node_id").Error; err != nil {
		return ""
	}
	return setting.Value
}

func (s *SettingsService) ClearMasterNode() error {
	return s.db.Delete(&models.Setting{}, "key = ?", "master_node_id").Error
}

func (s *SettingsService) GetMasterNode() (*models.Node, error) {
	id := s.GetMasterNodeID()
	if id == "" {
		return nil, nil
	}
	var node models.Node
	if err := s.db.First(&node, "id = ?", id).Error; err != nil {
		return nil, nil
	}
	return &node, nil
}

// --- Users ---

func (s *SettingsService) ListUsers() ([]models.User, error) {
	var users []models.User
	if err := s.db.Order("created_at ASC").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

func (s *SettingsService) CreateUser(username, email, password, role string) (*models.User, error) {
	if role != models.RoleAdmin && role != models.RoleOperator && role != models.RoleViewer {
		return nil, errors.New("invalid role")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	user := &models.User{
		ID:        uuid.New().String(),
		Username:  username,
		Email:     email,
		Password:  string(hash),
		Role:      role,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.db.Create(user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

func (s *SettingsService) ChangePassword(userID, newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.db.Model(&models.User{}).
		Where("id = ?", userID).
		Updates(map[string]interface{}{
			"password":   string(hash),
			"updated_at": time.Now(),
		}).Error
}

func (s *SettingsService) ChangeRole(userID, role string) error {
	if role != models.RoleAdmin && role != models.RoleOperator && role != models.RoleViewer {
		return errors.New("invalid role")
	}
	return s.db.Model(&models.User{}).
		Where("id = ?", userID).
		Updates(map[string]interface{}{
			"role":       role,
			"updated_at": time.Now(),
		}).Error
}

func (s *SettingsService) DeleteUser(userID, requesterID string) error {
	if userID == requesterID {
		return errors.New("cannot delete your own account")
	}
	return s.db.Delete(&models.User{}, "id = ?", userID).Error
}