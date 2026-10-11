package services

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-webauthn/webauthn/protocol"
)

const testOrigin = "https://hub.example.com"

// softKey is a software passkey: what a phone or security key does, in Go.
type softKey struct {
	id    []byte
	priv  *ecdsa.PrivateKey
	count uint32
}

func newSoftKey(t *testing.T) *softKey {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	rand.Read(id)
	return &softKey{id: id, priv: priv}
}

var b64 = base64.RawURLEncoding.EncodeToString

func clientData(typ, challenge, origin string) []byte {
	b, _ := json.Marshal(map[string]string{"type": typ, "challenge": challenge, "origin": origin})
	return b
}

func authData(rpID string, flags byte, count uint32, attested []byte) []byte {
	h := sha256.Sum256([]byte(rpID))
	out := append([]byte{}, h[:]...)
	out = append(out, flags)
	out = binary.BigEndian.AppendUint32(out, count)
	return append(out, attested...)
}

const (
	flagUP = 0x01
	flagUV = 0x04
	flagAT = 0x40
)

// create answers navigator.credentials.create.
func (k *softKey) create(t *testing.T, opts *protocol.CredentialCreation, origin string) []byte {
	t.Helper()
	pub := map[int]interface{}{1: 2, 3: -7, -1: 1, -2: k.priv.X.FillBytes(make([]byte, 32)), -3: k.priv.Y.FillBytes(make([]byte, 32))}
	cose, _ := cbor.Marshal(pub)
	attested := make([]byte, 16) // AAGUID
	attested = binary.BigEndian.AppendUint16(attested, uint16(len(k.id)))
	attested = append(attested, k.id...)
	attested = append(attested, cose...)
	ad := authData(opts.Response.RelyingParty.ID, flagUP|flagUV|flagAT, 0, attested)
	att, _ := cbor.Marshal(map[string]interface{}{"fmt": "none", "attStmt": map[string]interface{}{}, "authData": ad})
	resp, _ := json.Marshal(map[string]interface{}{
		"id": b64(k.id), "rawId": b64(k.id), "type": "public-key",
		"response": map[string]string{
			"clientDataJSON":    b64(clientData("webauthn.create", opts.Response.Challenge.String(), origin)),
			"attestationObject": b64(att),
		},
	})
	return resp
}

// get answers navigator.credentials.get.
func (k *softKey) get(t *testing.T, opts *protocol.CredentialAssertion, origin, userID string) []byte {
	t.Helper()
	k.count++
	ad := authData(opts.Response.RelyingPartyID, flagUP|flagUV, k.count, nil)
	cd := clientData("webauthn.get", opts.Response.Challenge.String(), origin)
	h := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, ad...), h[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, k.priv, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	resp, _ := json.Marshal(map[string]interface{}{
		"id": b64(k.id), "rawId": b64(k.id), "type": "public-key",
		"response": map[string]string{
			"clientDataJSON": b64(cd), "authenticatorData": b64(ad),
			"signature": b64(sig), "userHandle": b64([]byte(userID)),
		},
	})
	return resp
}

func addPasskey(t *testing.T, svc *AuthService, key *softKey) {
	t.Helper()
	opts, cer, err := svc.BeginPasskeyRegistration("u1", "correct-horse", "", testOrigin)
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	if _, err := svc.FinishPasskeyRegistration("u1", cer, "Test key", key.create(t, opts, testOrigin)); err != nil {
		t.Fatalf("finish registration: %v", err)
	}
}

func TestPasskey_RegisterThenSignIn(t *testing.T) {
	svc := newAuthTestService(t)
	key := newSoftKey(t)
	addPasskey(t, svc, key)

	keys, _ := svc.ListPasskeys("u1")
	if len(keys) != 1 || keys[0].Name != "Test key" || keys[0].RPID != "hub.example.com" {
		t.Fatalf("stored passkeys: %+v", keys)
	}

	opts, cer, err := svc.BeginPasskeyLogin(testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	signIn := testOpts
	signIn.Remember = true
	user, token, err := svc.FinishPasskeyLogin(cer, key.get(t, opts, testOrigin, "u1"), signIn)
	if err != nil {
		t.Fatalf("passkey sign-in: %v", err)
	}
	if user.ID != "u1" {
		t.Errorf("signed in as %s", user.ID)
	}
	if _, err := svc.ValidateToken(token, testOpts.Client); err != nil {
		t.Errorf("the passkey session doesn't work: %v", err)
	}
	sessions, _ := svc.ListSessions("u1")
	if len(sessions) != 1 || sessions[0].Method != "passkey" || !sessions[0].Remember {
		t.Errorf("session: %+v", sessions)
	}

	// The same ceremony can't be used twice.
	if _, _, err := svc.FinishPasskeyLogin(cer, key.get(t, opts, testOrigin, "u1"), testOpts); err == nil {
		t.Error("a sign-in ceremony was used twice")
	}
}

func TestPasskey_RefusesTheWrongSiteOrKey(t *testing.T) {
	svc := newAuthTestService(t)
	key := newSoftKey(t)
	addPasskey(t, svc, key)

	// Answered at another origin than the one the sign-in began at.
	opts, cer, _ := svc.BeginPasskeyLogin(testOrigin)
	if _, _, err := svc.FinishPasskeyLogin(cer, key.get(t, opts, "https://evil.example.net", "u1"), testOpts); err == nil {
		t.Error("an answer from another origin signed in")
	}
	// A key the Hub doesn't know.
	opts, cer, _ = svc.BeginPasskeyLogin(testOrigin)
	if _, _, err := svc.FinishPasskeyLogin(cer, newSoftKey(t).get(t, opts, testOrigin, "u1"), testOpts); err == nil {
		t.Error("an unknown passkey signed in")
	}
	// The passkey only works for the domain it was made at.
	other := "https://other.example.org"
	opts, cer, _ = svc.BeginPasskeyLogin(other)
	if _, _, err := svc.FinishPasskeyLogin(cer, key.get(t, opts, other, "u1"), testOpts); err == nil {
		t.Error("a passkey worked at a domain it wasn't made for")
	}
	// Removed passkeys stop working.
	keys, _ := svc.ListPasskeys("u1")
	if err := svc.DeletePasskey("u1", keys[0].ID); err != nil {
		t.Fatal(err)
	}
	opts, cer, _ = svc.BeginPasskeyLogin(testOrigin)
	if _, _, err := svc.FinishPasskeyLogin(cer, key.get(t, opts, testOrigin, "u1"), testOpts); err == nil {
		t.Error("a removed passkey signed in")
	}
}

func TestPasskey_AddingOneNeedsThePassword(t *testing.T) {
	svc := newAuthTestService(t)
	if _, _, err := svc.BeginPasskeyRegistration("u1", "wrong", "", testOrigin); err == nil {
		t.Error("a passkey could be added without the password")
	}
	setUp2FA(t, svc)
	if _, _, err := svc.BeginPasskeyRegistration("u1", "correct-horse", "", testOrigin); err == nil {
		t.Error("with two-factor on, a passkey could be added without a code")
	}
}

func TestRelyingParty_NeedsHTTPSAndADomain(t *testing.T) {
	for _, bad := range []string{"", "null", "http://hub.example.com", "https://203.0.113.4", "http://10.100.0.1:8080"} {
		if _, err := relyingParty(bad); err == nil {
			t.Errorf("%q was accepted for passkeys", bad)
		}
	}
	for _, good := range []string{"https://hub.example.com", "http://localhost:3000"} {
		if _, err := relyingParty(good); err != nil {
			t.Errorf("%q refused: %v", good, err)
		}
	}
}
