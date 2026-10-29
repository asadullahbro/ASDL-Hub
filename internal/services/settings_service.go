package services

import (
	"errors"
	"fmt"
	"log"
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

// Token scopes. A token acts with its owner's role, lowered to the role its
// scope names, so every check on the role holds for tokens too.
const (
	ScopeRead    = "read"    // view only (viewer)
	ScopeOperate = "operate" // deploy, restart, move (operator)
	ScopeAdmin   = "admin"   // everything its owner can do
)

var scopeRole = map[string]string{ScopeRead: models.RoleViewer, ScopeOperate: models.RoleOperator, ScopeAdmin: models.RoleAdmin}

var roleRank = map[string]int{models.RoleViewer: 1, models.RoleOperator: 2, models.RoleAdmin: 3}

// ValidScope reports whether scope is one a token can be made with.
func ValidScope(scope string) bool { _, ok := scopeRole[scope]; return ok }

// CapRole is the role a token of scope scope acts with for an owner of role
// role: the lower of the two. An empty scope (a token from before scopes)
// leaves the owner's role as it is.
func CapRole(role, scope string) string {
	limit, ok := scopeRole[scope]
	if !ok || roleRank[limit] >= roleRank[role] {
		return role
	}
	return limit
}

func (s *SettingsService) GeneratePermanentToken(name, createdBy, scope string) (string, *models.PermanentToken, error) {
	if !ValidScope(scope) {
		return "", nil, fmt.Errorf("scope must be read, operate or admin")
	}
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
		Token:     hashToken(tokenString), // the token itself is shown once, never stored
		TokenHint: "..." + hint,
		Scope:     scope,
		CreatedBy: createdBy,
		CreatedAt: time.Now(),
	}
	if err := s.db.Create(pt).Error; err != nil {
		return "", nil, err
	}
	return tokenString, pt, nil
}

// HashStoredTokens replaces permanent tokens that older versions stored
// as-is with their hash, so a copy of the database holds no usable token.
// Revoked ones too. Tokens keep working: ValidateToken looks up the hash.
func (s *SettingsService) HashStoredTokens() {
	var tokens []models.PermanentToken
	if err := s.db.Unscoped().Where("token LIKE ?", "%.%").Find(&tokens).Error; err != nil {
		log.Printf("⚠️ Could not read permanent tokens to hash them: %v", err)
		return
	}
	for _, t := range tokens {
		if err := s.db.Unscoped().Model(&models.PermanentToken{}).Where("id = ?", t.ID).
			Update("token", hashToken(t.Token)).Error; err != nil {
			log.Printf("⚠️ Could not hash permanent token %s: %v", t.ID, err)
		}
	}
	if len(tokens) > 0 {
		log.Printf("🔒 Stored %d permanent token(s) as hashes", len(tokens))
	}
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

func (s *SettingsService) ChangePassword(userID, newPassword, keepSessionID string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := s.db.Model(&models.User{}).
		Where("id = ?", userID).
		Updates(map[string]interface{}{
			"password":   string(hash),
			"updated_at": time.Now(),
		}).Error; err != nil {
		return err
	}
	// A new password signs the user out everywhere except keepSessionID
	// (the admin changing their own password stays signed in here).
	return s.db.Where("user_id = ? AND id <> ?", userID, keepSessionID).Delete(&models.Session{}).Error
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
	if err := s.db.Delete(&models.User{}, "id = ?", userID).Error; err != nil {
		return err
	}
	s.db.Where("user_id = ?", userID).Delete(&models.Passkey{})
	return s.db.Where("user_id = ?", userID).Delete(&models.Session{}).Error
}
