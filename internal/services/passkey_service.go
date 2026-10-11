package services

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/asdl/hub/internal/models"
)

// Passkeys (WebAuthn): sign in with the phone, laptop or security key's lock
// instead of a password and code. A passkey needs the authenticator to check
// it is you (fingerprint, face, PIN), so it is two factors on its own and
// skips the authenticator-app code.
//
// The relying party is the domain the dashboard was opened at, taken from the
// browser's Origin. Each passkey records that domain and only signs in there.
// Browsers only allow passkeys over https (or on localhost), with a name, not
// an IP address.

const ceremonyLife = 5 * time.Minute

var errPasskeyFailed = errors.New("that passkey didn't work; try again or sign in with your password")

// A ceremony is a begun registration or sign-in, waiting for the browser.
type ceremony struct {
	kind    string // register or login
	userID  string // register only
	origin  string
	data    webauthn.SessionData
	expires time.Time
}

type ceremonies struct {
	mu sync.Mutex
	m  map[string]*ceremony
}

func (c *ceremonies) put(cer *ceremony) (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(raw)
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, v := range c.m {
		if now.After(v.expires) {
			delete(c.m, k)
		}
	}
	if len(c.m) > 10000 {
		return "", errors.New("too many sign-ins in progress; try again shortly")
	}
	c.m[id] = cer
	return id, nil
}

// take returns a ceremony once; a second take gets nothing.
func (c *ceremonies) take(id, kind string) (*ceremony, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cer, ok := c.m[id]
	delete(c.m, id)
	if !ok || cer.kind != kind || time.Now().After(cer.expires) {
		return nil, false
	}
	return cer, true
}

// relyingParty is the WebAuthn configuration for a dashboard origin.
func relyingParty(origin string) (*webauthn.WebAuthn, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.Path != "" {
		return nil, errors.New("passkeys need the dashboard opened in a browser")
	}
	host := u.Hostname()
	secure := u.Scheme == "https" || (u.Scheme == "http" && host == "localhost")
	if !secure || net.ParseIP(host) != nil {
		return nil, errors.New("passkeys need the Hub opened over https at a domain name, not an IP address")
	}
	return webauthn.New(&webauthn.Config{
		RPID:          host,
		RPDisplayName: "ASDL Hub",
		RPOrigins:     []string{u.Scheme + "://" + u.Host},
	})
}

// passkeyUser is a user as the webauthn library sees one.
type passkeyUser struct {
	user  *models.User
	creds []webauthn.Credential
}

func (p *passkeyUser) WebAuthnID() []byte                         { return []byte(p.user.ID) }
func (p *passkeyUser) WebAuthnName() string                       { return p.user.Username }
func (p *passkeyUser) WebAuthnDisplayName() string                { return p.user.Username }
func (p *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return p.creds }

func credentialKey(id []byte) string { return base64.RawURLEncoding.EncodeToString(id) }

// BeginPasskeyRegistration starts adding a passkey. It asks for the password,
// and the authenticator-app code when that is on, so a stolen session can't
// plant a passkey that outlives it.
func (s *AuthService) BeginPasskeyRegistration(userID, password, code, origin string) (*protocol.CredentialCreation, string, error) {
	var user models.User
	if err := s.db.First(&user, "id = ?", userID).Error; err != nil {
		return nil, "", errors.New("user not found")
	}
	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)) != nil {
		return nil, "", errors.New("incorrect password")
	}
	if user.TOTPEnabled {
		if err := s.checkSecondFactor(&user, code); err != nil {
			return nil, "", err
		}
	}
	wa, err := relyingParty(origin)
	if err != nil {
		return nil, "", err
	}
	var existing []models.Passkey
	s.db.Where("user_id = ?", userID).Find(&existing)
	exclude := make([]protocol.CredentialDescriptor, 0, len(existing))
	for _, pk := range existing {
		if id, err := base64.RawURLEncoding.DecodeString(pk.CredentialID); err == nil {
			exclude = append(exclude, protocol.CredentialDescriptor{Type: protocol.PublicKeyCredentialType, CredentialID: id})
		}
	}
	creation, data, err := wa.BeginRegistration(&passkeyUser{user: &user},
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationRequired,
		}),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
		webauthn.WithExclusions(exclude),
	)
	if err != nil {
		return nil, "", err
	}
	id, err := s.passkeyCeremonies.put(&ceremony{
		kind: "register", userID: userID, origin: origin, data: *data, expires: time.Now().Add(ceremonyLife),
	})
	return creation, id, err
}

// FinishPasskeyRegistration saves the passkey the browser made.
func (s *AuthService) FinishPasskeyRegistration(userID, ceremonyID, name string, response []byte) (*models.Passkey, error) {
	cer, ok := s.passkeyCeremonies.take(ceremonyID, "register")
	if !ok || cer.userID != userID {
		return nil, errors.New("that took too long; start again")
	}
	var user models.User
	if err := s.db.First(&user, "id = ?", userID).Error; err != nil {
		return nil, errors.New("user not found")
	}
	wa, err := relyingParty(cer.origin)
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		return nil, errors.New("the browser's answer couldn't be read")
	}
	cred, err := wa.CreateCredential(&passkeyUser{user: &user}, cer.data, parsed)
	if err != nil {
		return nil, errors.New("the passkey couldn't be verified")
	}
	stored, err := json.Marshal(cred)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Passkey"
	}
	if len(name) > 100 {
		name = name[:100]
	}
	pk := &models.Passkey{
		ID:           uuid.New().String(),
		UserID:       userID,
		Name:         name,
		CredentialID: credentialKey(cred.ID),
		RPID:         wa.Config.RPID,
		Credential:   string(stored),
		CreatedAt:    time.Now(),
	}
	if err := s.db.Create(pk).Error; err != nil {
		return nil, errors.New("that passkey is already added")
	}
	return pk, nil
}

// BeginPasskeyLogin starts a sign-in where the browser offers whichever
// passkeys it has for this site; no username is needed.
func (s *AuthService) BeginPasskeyLogin(origin string) (*protocol.CredentialAssertion, string, error) {
	wa, err := relyingParty(origin)
	if err != nil {
		return nil, "", err
	}
	assertion, data, err := wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, "", err
	}
	id, err := s.passkeyCeremonies.put(&ceremony{
		kind: "login", origin: origin, data: *data, expires: time.Now().Add(ceremonyLife),
	})
	return assertion, id, err
}

// FinishPasskeyLogin checks the browser's answer and signs the user in.
func (s *AuthService) FinishPasskeyLogin(ceremonyID string, response []byte, opts SessionOptions) (*models.User, string, error) {
	cer, ok := s.passkeyCeremonies.take(ceremonyID, "login")
	if !ok {
		return nil, "", errors.New("that took too long; try again")
	}
	wa, err := relyingParty(cer.origin)
	if err != nil {
		return nil, "", err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		return nil, "", errPasskeyFailed
	}

	var pk models.Passkey
	var user models.User
	find := func(rawID, userHandle []byte) (webauthn.User, error) {
		if err := s.db.First(&pk, "credential_id = ?", credentialKey(rawID)).Error; err != nil {
			return nil, errPasskeyFailed
		}
		if pk.RPID != wa.Config.RPID || string(userHandle) != pk.UserID {
			return nil, errPasskeyFailed
		}
		if err := s.db.First(&user, "id = ?", pk.UserID).Error; err != nil {
			return nil, errPasskeyFailed
		}
		var cred webauthn.Credential
		if err := json.Unmarshal([]byte(pk.Credential), &cred); err != nil {
			return nil, errPasskeyFailed
		}
		return &passkeyUser{user: &user, creds: []webauthn.Credential{cred}}, nil
	}
	cred, err := wa.ValidateDiscoverableLogin(find, cer.data, parsed)
	if err != nil {
		return nil, "", errPasskeyFailed
	}
	// A signature count that went backwards means the key was copied.
	if cred.Authenticator.CloneWarning {
		return nil, "", errors.New("this passkey looks cloned; it was refused. Remove it in Settings and add it again")
	}
	now := time.Now()
	if stored, err := json.Marshal(cred); err == nil {
		s.db.Model(&models.Passkey{}).Where("id = ?", pk.ID).
			Updates(map[string]interface{}{"credential": string(stored), "last_used_at": now})
	}
	opts.Method = "passkey"
	token, err := s.issueSession(&user, opts)
	if err != nil {
		return nil, "", err
	}
	return &user, token, nil
}

// ListPasskeys is a user's passkeys, oldest first.
func (s *AuthService) ListPasskeys(userID string) ([]models.Passkey, error) {
	var out []models.Passkey
	err := s.db.Where("user_id = ?", userID).Order("created_at").Find(&out).Error
	return out, err
}

// DeletePasskey removes one of a user's passkeys.
func (s *AuthService) DeletePasskey(userID, id string) error {
	res := s.db.Where("id = ? AND user_id = ?", id, userID).Delete(&models.Passkey{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("no such passkey")
	}
	return nil
}
