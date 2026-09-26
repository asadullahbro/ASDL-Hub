package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSecretEnvVars_EncryptedAtRestMaskedInJSON(t *testing.T) {
	if err := SetSecretsKey("k1"); err != nil {
		t.Fatal(err)
	}
	vars := SecretEnvVars{{Key: "DB_PASSWORD", Value: "hunter2"}}

	stored, err := vars.Value()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored.(string), "hunter2") {
		t.Fatalf("value stored in plain text: %s", stored)
	}

	var loaded SecretEnvVars
	if err := loaded.Scan(stored); err != nil {
		t.Fatal(err)
	}
	if loaded[0].Value != "hunter2" {
		t.Errorf("round trip = %q", loaded[0].Value)
	}

	out, _ := json.Marshal(struct {
		Env SecretEnvVars `json:"env"`
	}{loaded})
	if strings.Contains(string(out), "hunter2") || !strings.Contains(string(out), MaskedValue) {
		t.Errorf("JSON must mask values: %s", out)
	}
}

func TestSecretEnvVars_ReadsLegacyPlainValues(t *testing.T) {
	var loaded SecretEnvVars
	if err := loaded.Scan(`[{"key":"A","value":"plain"}]`); err != nil {
		t.Fatal(err)
	}
	if loaded[0].Value != "plain" {
		t.Errorf("got %q", loaded[0].Value)
	}
}

func TestMergeMasked(t *testing.T) {
	current := []EnvVar{{Key: "A", Value: "a"}, {Key: "B", Value: "b"}}
	got := MergeMasked([]EnvVar{{Key: "A", Value: MaskedValue}, {Key: "C", Value: "c"}, {Key: "Z", Value: MaskedValue}}, current)
	want := []EnvVar{{Key: "A", Value: "a"}, {Key: "C", Value: "c"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v (B removed, unknown masked Z dropped)", got, want)
	}
}
