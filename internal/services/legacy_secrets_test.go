package services

import (
	"strings"
	"testing"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/testutil"
)

func TestEncryptLegacySecrets(t *testing.T) {
	db := testutil.NewDB(t, &models.GitHubToken{}, &models.Project{})
	// Rows as older hubs wrote them: plain text.
	db.Exec("INSERT INTO git_hub_tokens (id, label, token) VALUES ('t1', 'ci', 'ghp_plaintexttoken123')")
	db.Exec(`INSERT INTO projects (id, name, node_id, env_vars) VALUES ('p1', 'api', '', '[{"key":"A","value":"plain"}]')`)

	models.EncryptLegacySecrets(db)

	var rawToken, rawEnv string
	db.Raw("SELECT token FROM git_hub_tokens WHERE id = 't1'").Scan(&rawToken)
	db.Raw("SELECT env_vars FROM projects WHERE id = 'p1'").Scan(&rawEnv)
	if strings.Contains(rawToken, "plaintext") || strings.Contains(rawEnv, "plain") {
		t.Fatalf("still plain text: token=%s env=%s", rawToken, rawEnv)
	}

	var tok models.GitHubToken
	db.First(&tok, "id = ?", "t1")
	var p models.Project
	db.First(&p, "id = ?", "p1")
	if tok.Token != "ghp_plaintexttoken123" || p.EnvVars[0].Value != "plain" {
		t.Errorf("values must still read back: %q %v", tok.Token, p.EnvVars)
	}
	if models.MaskToken(string(tok.Token)) != "ghp_...n123" || models.MaskToken("short") != models.MaskedValue {
		t.Error("MaskToken")
	}
}
