package secret

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureEnvFileCreates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", ".env")

	created, err := EnsureEnvFile(path)
	if err != nil {
		t.Fatalf("EnsureEnvFile failed: %v", err)
	}
	if !created {
		t.Error("expected created=true for a fresh file")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected perms 0600, got %v", info.Mode().Perm())
	}

	secrets, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	for _, key := range requiredKeys {
		if secrets[key] == "" {
			t.Errorf("expected generated value for %s", key)
		}
	}

	// Sanity check key strength: 32 bytes base64 raw url -> 43 chars
	if len(secrets[JWTSecret]) != 43 {
		t.Errorf("expected 43-char base64 secret, got %d", len(secrets[JWTSecret]))
	}
}

func TestEnsureEnvFileIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")

	if _, err := EnsureEnvFile(path); err != nil {
		t.Fatalf("EnsureEnvFile failed: %v", err)
	}
	first, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}

	created, err := EnsureEnvFile(path)
	if err != nil {
		t.Fatalf("second EnsureEnvFile failed: %v", err)
	}
	if created {
		t.Error("expected created=false when the file already exists")
	}

	second, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	for key, val := range first {
		if second[key] != val {
			t.Errorf("secret %s regenerated on existing file: %q -> %q", key, val, second[key])
		}
	}
}

func TestEnsureEnvFileAddsMissingKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "OTHER_VAR=hello\nAPI_KEY_SECRET=user-provided-key\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to seed .env: %v", err)
	}

	created, err := EnsureEnvFile(path)
	if err != nil {
		t.Fatalf("EnsureEnvFile failed: %v", err)
	}
	if created {
		t.Error("expected created=false when the file already exists")
	}

	secrets, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	if secrets[APIKeySecret] != "user-provided-key" {
		t.Errorf("existing value must be preserved, got %q", secrets[APIKeySecret])
	}
	if secrets[JWTSecret] == "" {
		t.Error("expected missing JWT_SECRET to be generated")
	}

	// OTHER_VAR line must survive the merge
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !strings.Contains(string(data), "OTHER_VAR=hello") {
		t.Error("unrelated .env content was lost during merge")
	}
	if !strings.Contains(string(data), "API_KEY_SECRET=user-provided-key") {
		t.Error("existing key/value line was lost during merge")
	}
}

func TestEnsureEnvFileErrorOnInvalidPath(t *testing.T) {
	dir := t.TempDir()
	// Make the target path a directory: provisioning must fail cleanly.
	path := filepath.Join(dir, ".env")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := EnsureEnvFile(path); err == nil {
		t.Error("expected error when path is a directory")
	}
}

func TestLoadFileParsing(t *testing.T) {
	content := `# full-line comment
export API_KEY_SECRET="quoted value"
JWT_SECRET='single quoted'
PLAIN=alpha123
INLINE=with-comment # trailing comment
EMPTY=
ONELINE_WITH_NOMETHOD
=NOKEY
`
	out := Parse(content)
	if got := out["API_KEY_SECRET"]; got != "quoted value" {
		t.Errorf("double quotes not stripped: %q", got)
	}
	if got := out["JWT_SECRET"]; got != "single quoted" {
		t.Errorf("single quotes not stripped: %q", got)
	}
	if got := out["PLAIN"]; got != "alpha123" {
		t.Errorf("plain value broken: %q", got)
	}
	if got := out["INLINE"]; got != "with-comment" {
		t.Errorf("inline comment not stripped: %q", got)
	}
	if val, ok := out["EMPTY"]; !ok || val != "" {
		t.Errorf("empty-valued key must parse with empty value: %q ok=%v", val, ok)
	}
}

func TestGenerateKeyUnique(t *testing.T) {
	a, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	b, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	if a == "" || b == "" {
		t.Error("generated keys must not be empty")
	}
	if a == b {
		t.Error("generated keys must be unique")
	}
}

func TestDefaultPathOverride(t *testing.T) {
	t.Setenv(EnvFileEnvVar, filepath.Join(t.TempDir(), "custom.env"))
	if got := DefaultPath(); got != os.Getenv(EnvFileEnvVar) {
		t.Errorf("env override not honored: %q", got)
	}
}
