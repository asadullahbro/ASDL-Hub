package models

import (
	"gorm.io/gorm"
	"time"
)

const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"
)

type User struct {
	ID       string `gorm:"primaryKey;size:36" json:"id"`
	Username string `gorm:"uniqueIndex;size:50;not null" json:"username"`
	Email    string `gorm:"uniqueIndex;size:100;not null" json:"email"`
	Password string `gorm:"not null" json:"-"`
	Role     string `gorm:"size:20;default:viewer" json:"role"`

	// Two-factor sign-in with an authenticator app. The secret is stored
	// encrypted; it exists from the moment setup begins but only counts once
	// TOTPEnabled is set by confirming a code.
	TOTPEnabled   bool   `gorm:"not null;default:false" json:"totp_enabled"`
	TOTPSecret    string `gorm:"type:text" json:"-"`
	TOTPLastStep  int64  `gorm:"not null;default:0" json:"-"`
	RecoveryCodes string `gorm:"type:text" json:"-"` // JSON list of bcrypt hashes

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// Session is one signed-in browser (or command line). The session token
// names it, so deleting the row signs that device out.
type Session struct {
	ID     string `gorm:"primaryKey;size:36" json:"id"`
	UserID string `gorm:"index;not null" json:"user_id"`
	// SHA-256 of the token, never the token itself.
	Token     string `gorm:"uniqueIndex;size:500;not null" json:"-"`
	IP        string `gorm:"size:45" json:"ip"` // where it signed in from
	UserAgent string `gorm:"size:255" json:"user_agent"`
	// Browser and OS, e.g. "Firefox on Linux". The token only works from a
	// client that still matches.
	Device     string    `gorm:"size:100;not null;default:''" json:"device"`
	Method     string    `gorm:"size:20;not null;default:''" json:"method"` // password, passkey or cli
	Remember   bool      `gorm:"not null;default:false" json:"remember"`
	LastIP     string    `gorm:"size:45;not null;default:''" json:"last_ip"`
	LastUsedAt time.Time `json:"last_used_at"`
	ExpiresAt  time.Time `gorm:"index" json:"expires_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// Passkey is a WebAuthn credential a user signs in with instead of a
// password and code.
type Passkey struct {
	ID     string `gorm:"primaryKey;size:36" json:"id"`
	UserID string `gorm:"index;size:36;not null" json:"-"`
	Name   string `gorm:"size:100;not null" json:"name"`
	// base64url of the credential ID the authenticator reports.
	CredentialID string `gorm:"uniqueIndex;size:1400;not null" json:"-"`
	// The domain it was made for; it only signs in there.
	RPID string `gorm:"size:255;not null" json:"rp_id"`
	// The webauthn.Credential as JSON: public key, sign count, flags.
	Credential string     `gorm:"type:text;not null" json:"-"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// Setting — generic key/value store for global config
type Setting struct {
	Key       string    `gorm:"primaryKey;size:100" json:"key"`
	Value     string    `gorm:"type:text" json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PermanentToken — long-lived API tokens
type PermanentToken struct {
	ID        string `gorm:"primaryKey;size:36" json:"id"`
	Name      string `gorm:"size:100;not null" json:"name"`
	Token     string `gorm:"uniqueIndex;size:500;not null" json:"-"`
	TokenHint string `gorm:"size:20" json:"token_hint"` // last 8 chars shown in UI
	// Scope caps what the token may do, whatever its owner can: read, operate
	// or admin. Tokens made before scopes existed have none and keep full access.
	Scope     string         `gorm:"size:20;default:admin" json:"scope"`
	CreatedBy string         `gorm:"size:36" json:"created_by"`
	CreatedAt time.Time      `json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
