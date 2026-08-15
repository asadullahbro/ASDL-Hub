package github

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"github.com/asdl/hub/internal/models"
)

const githubAPIBase = "https://api.github.com"

// AppClient handles GitHub App authentication.
// Credentials are read from the Setting table on every call —
// no long-lived state, no cached tokens.
type AppClient struct {
	db *gorm.DB
}

func NewAppClient(db *gorm.DB) *AppClient {
	return &AppClient{db: db}
}

// appCredentials reads App ID and private key from settings.
func (a *AppClient) appCredentials() (appID string, privateKey *rsa.PrivateKey, err error) {
	var idSetting, keySetting models.Setting

	if err = a.db.First(&idSetting, "key = ?", "github_app_id").Error; err != nil {
		return "", nil, fmt.Errorf("github_app_id not configured")
	}
	if err = a.db.First(&keySetting, "key = ?", "github_app_private_key").Error; err != nil {
		return "", nil, fmt.Errorf("github_app_private_key not configured")
	}

	block, _ := pem.Decode([]byte(keySetting.Value))
	if block == nil {
		return "", nil, fmt.Errorf("github_app_private_key is not valid PEM")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS8
		parsed, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return "", nil, fmt.Errorf("failed to parse private key: %v", err)
		}
		var ok bool
		key, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return "", nil, fmt.Errorf("private key is not RSA")
		}
	}

	return idSetting.Value, key, nil
}

// generateAppJWT creates a short-lived JWT signed with the App private key.
// GitHub requires this to authenticate as the App itself.
func (a *AppClient) generateAppJWT() (string, error) {
	appID, privateKey, err := a.appCredentials()
	if err != nil {
		return "", err
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iat": now.Add(-60 * time.Second).Unix(), // issued 60s ago to account for clock skew
		"exp": now.Add(9 * time.Minute).Unix(),   // max 10 minutes
		"iss": appID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign app JWT: %w", err)
	}

	return signed, nil
}

// InstallationToken generates a short-lived installation access token for a given installation ID.
// Call this whenever you need to authenticate as the installation — never cache the result.
func (a *AppClient) InstallationToken(installationID int64) (string, error) {
	appJWT, err := a.generateAppJWT()
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", githubAPIBase, installationID)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("installation token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("GitHub returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}
	if result.Token == "" {
		return "", fmt.Errorf("GitHub returned empty token")
	}

	return result.Token, nil
}

// WebhookSecret reads the webhook secret from settings.
func (a *AppClient) WebhookSecret() (string, error) {
	var setting models.Setting
	if err := a.db.First(&setting, "key = ?", "github_webhook_secret").Error; err != nil {
		return "", fmt.Errorf("github_webhook_secret not configured")
	}
	return setting.Value, nil
}
