package github

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// verifyRS256 verifies the RS256 signature of a JWT.
// It re-encodes the header.payload and checks against the signature part.
func verifyRS256(rawToken string, pub *rsa.PublicKey) error {
	parts := splitJWT(rawToken)
	if len(parts) != 3 {
		return fmt.Errorf("malformed JWT")
	}

	// The signed message is the raw "header.payload" string (not decoded).
	signingInput := parts[0] + "." + parts[1]

	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("invalid signature encoding: %w", err)
	}

	digest := sha256.Sum256([]byte(signingInput))

	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sigBytes); err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}

	return nil
}
