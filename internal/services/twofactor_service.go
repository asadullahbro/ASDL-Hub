package services

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/asdl/hub/internal/models"
)

// Sign-in with an authenticator app (TOTP, RFC 6238: SHA-1, 6 digits, 30s).
// A user with it on gets a short-lived "mfa" token from the password step and
// exchanges it, with a code, for a session token. The mfa token is not a
// session: ValidateToken refuses it.

const (
	totpPeriod     = 30
	totpDigits     = 6
	mfaTokenLife   = 5 * time.Minute
	recoveryCount  = 10
	totpIssuerName = "ASDL Hub"
)

// ErrTwoFactorRequired is returned by Login when the password was right but a
// code is still needed.
var ErrTwoFactorRequired = errors.New("two-factor code required")

// bcrypt cost of recovery code hashes; tests lower it.
var recoveryCost = bcrypt.DefaultCost

var recoveryEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// hotp is the RFC 4226 code for a counter.
func hotp(key []byte, counter uint64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	n := (binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff) % 1_000_000
	return fmt.Sprintf("%0*d", totpDigits, n)
}

// totpStep is the time step a moment falls in.
func totpStep(t time.Time) int64 { return t.Unix() / totpPeriod }

// checkTOTP reports the time step a code is valid for, accepting one step
// either side for clock drift. Steps at or before lastStep are refused so a
// code (say one seen over a shoulder) can't be used twice.
func checkTOTP(key []byte, code string, now time.Time, lastStep int64) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return 0, false
	}
	cur := totpStep(now)
	for step := cur - 1; step <= cur+1; step++ {
		if step <= lastStep {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(code), []byte(hotp(key, uint64(step)))) == 1 {
			return step, true
		}
	}
	return 0, false
}

// The secret is encrypted with a key derived from the Hub's JWT secret, so a
// copy of the database alone doesn't hold anyone's second factor.
func (s *AuthService) totpKey() []byte {
	sum := sha256.Sum256([]byte("asdl-hub totp v1:" + s.jwtSecret))
	return sum[:]
}

func (s *AuthService) sealSecret(secret []byte) (string, error) {
	block, err := aes.NewCipher(s.totpKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, secret, nil)), nil
}

func (s *AuthService) openSecret(sealed string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(s.totpKey())
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("stored secret is malformed")
	}
	return gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
}

// BeginTwoFactor starts setup: it makes a new secret (replacing any earlier
// unconfirmed one) and returns it for the authenticator app. Nothing is
// required at sign-in until EnableTwoFactor confirms a code.
func (s *AuthService) BeginTwoFactor(userID string) (secret, uri string, err error) {
	var user models.User
	if err := s.db.First(&user, "id = ?", userID).Error; err != nil {
		return "", "", errors.New("user not found")
	}
	if user.TOTPEnabled {
		return "", "", errors.New("two-factor is already on; turn it off first to set it up again")
	}
	key := make([]byte, 20)
	if _, err := rand.Read(key); err != nil {
		return "", "", err
	}
	sealed, err := s.sealSecret(key)
	if err != nil {
		return "", "", err
	}
	if err := s.db.Model(&user).Updates(map[string]interface{}{"totp_secret": sealed, "totp_last_step": 0}).Error; err != nil {
		return "", "", err
	}
	secret = recoveryEncoding.EncodeToString(key)
	label := url.PathEscape(totpIssuerName + ":" + user.Username)
	uri = fmt.Sprintf("otpauth://totp/%s?secret=%s&issuer=%s&algorithm=SHA1&digits=%d&period=%d",
		label, secret, url.QueryEscape(totpIssuerName), totpDigits, totpPeriod)
	return secret, uri, nil
}

// EnableTwoFactor turns it on once the user proves the app shows the right
// code, and returns the one-time recovery codes (shown only now).
func (s *AuthService) EnableTwoFactor(userID, code string) ([]string, error) {
	var user models.User
	if err := s.db.First(&user, "id = ?", userID).Error; err != nil {
		return nil, errors.New("user not found")
	}
	if user.TOTPEnabled {
		return nil, errors.New("two-factor is already on")
	}
	if user.TOTPSecret == "" {
		return nil, errors.New("start the setup first")
	}
	key, err := s.openSecret(user.TOTPSecret)
	if err != nil {
		return nil, errors.New("start the setup again")
	}
	step, ok := checkTOTP(key, code, time.Now(), 0)
	if !ok {
		return nil, errors.New("that code is wrong or expired")
	}
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	stored, _ := json.Marshal(hashes)
	err = s.db.Model(&user).Updates(map[string]interface{}{
		"totp_enabled": true, "totp_last_step": step, "recovery_codes": string(stored),
	}).Error
	return codes, err
}

// DisableTwoFactor turns it off; it needs the password and a current code (or
// a recovery code), so a stolen session alone can't remove the second factor.
func (s *AuthService) DisableTwoFactor(userID, password, code string) error {
	var user models.User
	if err := s.db.First(&user, "id = ?", userID).Error; err != nil {
		return errors.New("user not found")
	}
	if !user.TOTPEnabled {
		return errors.New("two-factor is not on")
	}
	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)) != nil {
		return errors.New("incorrect password")
	}
	if err := s.checkSecondFactor(&user, code); err != nil {
		return err
	}
	return s.ResetTwoFactor(userID)
}

// ResetTwoFactor clears a user's two-factor settings with no questions asked:
// for an admin helping someone who lost their phone and recovery codes.
func (s *AuthService) ResetTwoFactor(userID string) error {
	return s.db.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"totp_enabled": false, "totp_secret": "", "totp_last_step": 0, "recovery_codes": "",
	}).Error
}

// MFAToken is what the password step hands out when a code is still needed.
func (s *AuthService) MFAToken(user *models.User) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID,
		"type":    "mfa",
		"exp":     time.Now().Add(mfaTokenLife).Unix(),
	}).SignedString([]byte(s.jwtSecret))
}

// MFAUser identifies who an mfa token is for.
func (s *AuthService) MFAUser(mfaToken string) (*models.User, error) {
	token, err := jwt.Parse(mfaToken, func(*jwt.Token) (interface{}, error) { return []byte(s.jwtSecret), nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || !token.Valid {
		return nil, errors.New("sign in again")
	}
	claims, _ := token.Claims.(jwt.MapClaims)
	if t, _ := claims["type"].(string); t != "mfa" {
		return nil, errors.New("sign in again")
	}
	id, _ := claims["user_id"].(string)
	var user models.User
	if err := s.db.First(&user, "id = ?", id).Error; err != nil || !user.TOTPEnabled {
		return nil, errors.New("sign in again")
	}
	return &user, nil
}

// CompleteTwoFactorLogin checks the code for the user an mfa token names and
// returns the session token.
func (s *AuthService) CompleteTwoFactorLogin(user *models.User, code string) (string, error) {
	if err := s.checkSecondFactor(user, code); err != nil {
		return "", err
	}
	return s.sessionToken(user)
}

var errBadCode = errors.New("that code is wrong or expired")

// checkSecondFactor accepts a current app code, or an unused recovery code
// (spent by use). Both are single-use.
func (s *AuthService) checkSecondFactor(user *models.User, code string) error {
	code = strings.TrimSpace(code)
	if len(code) == totpDigits && strings.Trim(code, "0123456789") == "" {
		key, err := s.openSecret(user.TOTPSecret)
		if err != nil {
			return errBadCode
		}
		step, ok := checkTOTP(key, code, time.Now(), user.TOTPLastStep)
		if !ok {
			return errBadCode
		}
		// Only one of two simultaneous uses of the same code can win.
		res := s.db.Model(&models.User{}).Where("id = ? AND totp_last_step < ?", user.ID, step).Update("totp_last_step", step)
		if res.Error != nil || res.RowsAffected != 1 {
			return errBadCode
		}
		return nil
	}

	var hashes []string
	if json.Unmarshal([]byte(user.RecoveryCodes), &hashes) != nil {
		return errBadCode
	}
	norm := normalizeRecovery(code)
	for i, h := range hashes {
		if bcrypt.CompareHashAndPassword([]byte(h), []byte(norm)) != nil {
			continue
		}
		rest, _ := json.Marshal(append(append([]string{}, hashes[:i]...), hashes[i+1:]...))
		res := s.db.Model(&models.User{}).Where("id = ? AND recovery_codes = ?", user.ID, user.RecoveryCodes).Update("recovery_codes", string(rest))
		if res.Error != nil || res.RowsAffected != 1 {
			return errBadCode
		}
		return nil
	}
	return errBadCode
}

func normalizeRecovery(code string) string {
	return strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(code))
}

// newRecoveryCodes makes the codes to show once (xxxxx-xxxxx) and the hashes
// to keep.
func newRecoveryCodes() (codes, hashes []string, err error) {
	for i := 0; i < recoveryCount; i++ {
		raw := make([]byte, 7)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, err
		}
		plain := strings.ToLower(recoveryEncoding.EncodeToString(raw))[:10]
		h, err := bcrypt.GenerateFromPassword([]byte(plain), recoveryCost)
		if err != nil {
			return nil, nil, err
		}
		codes = append(codes, plain[:5]+"-"+plain[5:])
		hashes = append(hashes, string(h))
	}
	return codes, hashes, nil
}
