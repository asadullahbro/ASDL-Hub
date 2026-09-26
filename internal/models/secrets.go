package models

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
)

// MaskedValue replaces secret values in API responses. Sending it back in an
// update keeps the stored value.
const MaskedValue = "********"

const encryptedPrefix = "enc:v1:"

var (
	secretsMu   sync.RWMutex
	secretsAEAD cipher.AEAD
)

// SetSecretsKey sets the key project secrets are encrypted with. Any string
// works; it is hashed to a 256-bit AES key.
func SetSecretsKey(key string) error {
	if key == "" {
		return errors.New("secrets key is empty")
	}
	sum := sha256.Sum256([]byte("asdl-hub/project-secrets/v1:" + key))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	secretsMu.Lock()
	secretsAEAD = aead
	secretsMu.Unlock()
	return nil
}

func encryptSecret(plain string) (string, error) {
	secretsMu.RLock()
	aead := secretsAEAD
	secretsMu.RUnlock()
	if aead == nil {
		return "", errors.New("secrets key not configured")
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, []byte(plain), nil)
	return encryptedPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// decryptSecret returns stored values without the prefix unchanged, so
// values saved before encryption was added still load.
func decryptSecret(stored string) (string, error) {
	if !strings.HasPrefix(stored, encryptedPrefix) {
		return stored, nil
	}
	secretsMu.RLock()
	aead := secretsAEAD
	secretsMu.RUnlock()
	if aead == nil {
		return "", errors.New("secrets key not configured")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, encryptedPrefix))
	if err != nil || len(raw) < aead.NonceSize() {
		return "", errors.New("malformed encrypted value")
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], nil)
	if err != nil {
		return "", errors.New("cannot decrypt value (was SECRETS_KEY or JWT_SECRET changed?)")
	}
	return string(plain), nil
}

func scanText(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("unsupported type %T", value)
	}
}

// SecretEnvVars holds a project's environment variables. Values are
// encrypted in the database and masked when marshalled to JSON; in memory
// they are plain text.
type SecretEnvVars []EnvVar

func (s SecretEnvVars) Value() (driver.Value, error) {
	out := make([]EnvVar, len(s))
	for i, e := range s {
		enc, err := encryptSecret(e.Value)
		if err != nil {
			return nil, err
		}
		out[i] = EnvVar{Key: e.Key, Value: enc}
	}
	b, err := json.Marshal(out)
	return string(b), err
}

func (s *SecretEnvVars) Scan(value interface{}) error {
	b, err := scanText(value)
	if err != nil || len(b) == 0 {
		*s = nil
		return err
	}
	var stored []EnvVar
	if err := json.Unmarshal(b, &stored); err != nil {
		return err
	}
	for i := range stored {
		plain, err := decryptSecret(stored[i].Value)
		if err != nil {
			// Don't fail the whole query over one value; the key is kept so
			// it is visible that the value needs setting again.
			log.Printf("⚠️ env var %s: %v", stored[i].Key, err)
			plain = ""
		}
		stored[i].Value = plain
	}
	*s = stored
	return nil
}

func (s SecretEnvVars) MarshalJSON() ([]byte, error) {
	masked := make([]EnvVar, len(s))
	for i, e := range s {
		masked[i] = EnvVar{Key: e.Key, Value: MaskedValue}
	}
	return json.Marshal(masked)
}

// MergeMasked returns updated with every masked value replaced by the value
// the same key has in current, so clients can send back what they received.
func MergeMasked(updated, current []EnvVar) []EnvVar {
	existing := make(map[string]string, len(current))
	for _, e := range current {
		existing[e.Key] = e.Value
	}
	out := make([]EnvVar, 0, len(updated))
	for _, e := range updated {
		if e.Value == MaskedValue {
			v, ok := existing[e.Key]
			if !ok {
				continue
			}
			e.Value = v
		}
		out = append(out, e)
	}
	return out
}

// EncryptedStrings is a []string stored encrypted. Unlike SecretEnvVars it
// marshals to JSON in plain text, because agents need the values; handlers
// that show it to users must redact it.
type EncryptedStrings []string

func (s EncryptedStrings) Value() (driver.Value, error) {
	out := make([]string, len(s))
	for i, v := range s {
		enc, err := encryptSecret(v)
		if err != nil {
			return nil, err
		}
		out[i] = enc
	}
	b, err := json.Marshal(out)
	return string(b), err
}

func (s *EncryptedStrings) Scan(value interface{}) error {
	b, err := scanText(value)
	if err != nil || len(b) == 0 {
		*s = nil
		return err
	}
	var stored []string
	if err := json.Unmarshal(b, &stored); err != nil {
		return err
	}
	for i := range stored {
		plain, err := decryptSecret(stored[i])
		if err != nil {
			log.Printf("⚠️ job environment: %v", err)
			plain = ""
		}
		stored[i] = plain
	}
	*s = stored
	return nil
}
