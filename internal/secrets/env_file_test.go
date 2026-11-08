package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestEnvFileProviderCreatesSecureFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secrets")
	path := filepath.Join(dir, ".env")

	p, err := NewEnvFileProvider(path)
	if err != nil {
		t.Fatalf("NewEnvFileProvider failed: %v", err)
	}
	if p.Path() != path {
		t.Fatalf("Path() = %q, want %q", p.Path(), path)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Errorf("env file permissions = %o, want 600", fileInfo.Mode().Perm())
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("secret directory permissions = %o, want 700", dirInfo.Mode().Perm())
	}
}

func TestEnvFileProviderSetManyPreservesUnmanagedContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	original := "# keep this comment\nOTHER_VAR=hello\nAPI_KEY_HASH=old\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	p, err := NewEnvFileProvider(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetMany(map[string]string{"API_KEY_HASH": "sha256:new"}); err != nil {
		t.Fatalf("SetMany failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"# keep this comment", "OTHER_VAR=hello", "API_KEY_HASH=sha256:new"} {
		if !strings.Contains(text, want) {
			t.Errorf("env file lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "API_KEY_HASH=old") {
		t.Errorf("old value survived replacement:\n%s", text)
	}

	value, err := p.Get("API_KEY_HASH")
	if err != nil || value != "sha256:new" {
		t.Fatalf("Get after SetMany = %q, %v", value, err)
	}
}

func TestEnvFileProviderAppendsMissingKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("EXISTING=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := NewEnvFileProvider(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetMany(map[string]string{"B": "2", "A": "1"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	if !strings.Contains(text, "A=1") || !strings.Contains(text, "B=2") {
		t.Fatalf("missing keys not appended:\n%s", text)
	}
	if strings.Index(text, "A=1") > strings.Index(text, "B=2") {
		t.Fatalf("appended keys are not deterministic:\n%s", text)
	}
}

func TestEnvFileProviderReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := NewEnvFileProvider(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("A=2\nB=3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if value, _ := p.Get("A"); value != "2" {
		t.Fatalf("A = %q, want 2", value)
	}
	if value, _ := p.Get("B"); value != "3" {
		t.Fatalf("B = %q, want 3", value)
	}
}

func TestEnvFileProviderReloadSnapshotIsStaged(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := NewEnvFileProvider(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("A=2\nB=3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := p.ReloadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := p.Get("A"); value != "1" {
		t.Fatalf("ReloadSnapshot must not publish values; A = %q", value)
	}

	if err := p.CommitReload(snapshot); err != nil {
		t.Fatal(err)
	}
	if value, _ := p.Get("A"); value != "2" {
		t.Fatalf("A = %q, want 2", value)
	}
	if value, _ := p.Get("B"); value != "3" {
		t.Fatalf("B = %q, want 3", value)
	}
}

func TestEnvFileProviderCommitReloadRejectsStaleSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := NewEnvFileProvider(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("A=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := p.ReloadSnapshot()
	if err != nil {
		t.Fatal(err)
	}

	// A newer write lands before the candidate is committed.
	if err := os.WriteFile(path, []byte("A=3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.CommitReload(snapshot); !errors.Is(err, ErrStaleSnapshot) {
		t.Fatalf("CommitReload error = %v, want ErrStaleSnapshot", err)
	}
	if value, _ := p.Get("A"); value != "1" {
		t.Fatalf("stale commit must not publish; A = %q", value)
	}
}

func TestEnvFileProviderReloadSnapshotRejectsInvalidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := NewEnvFileProvider(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("BAD-KEY=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ReloadSnapshot(); err == nil {
		t.Fatal("invalid secret reference must fail reload")
	}
	if value, _ := p.Get("A"); value != "1" {
		t.Fatalf("failed reload must not publish; A = %q", value)
	}
}

func TestEnvFileProviderRejectsUnsafeValues(t *testing.T) {
	p, err := NewEnvFileProvider(filepath.Join(t.TempDir(), ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetMany(map[string]string{"A": "line1\nline2"}); err == nil {
		t.Fatal("newline-containing secret must be rejected")
	}
	if err := p.SetMany(map[string]string{"BAD-KEY": "x"}); err == nil {
		t.Fatal("invalid reference must be rejected")
	}
}

func TestStagedProviderOverridesWithoutMutatingBase(t *testing.T) {
	base := NewMapProvider(map[string]string{"A": "old", "B": "keep"})
	staged := Staged(base, map[string]string{"A": "new"})

	if value, _ := staged.Get("A"); value != "new" {
		t.Fatalf("staged A = %q, want new", value)
	}
	if value, _ := staged.Get("B"); value != "keep" {
		t.Fatalf("staged B = %q, want keep", value)
	}
	if value, _ := base.Get("A"); value != "old" {
		t.Fatalf("base A was mutated: %q", value)
	}
	if err := staged.SetMany(map[string]string{"A": "x"}); err == nil {
		t.Fatal("staged provider must be read-only")
	}
}

func TestHashSecretRoundTrip(t *testing.T) {
	hash := HashSecret("super-secret")
	if !IsHash(hash) {
		t.Fatalf("hash %q lacks canonical prefix", hash)
	}
	if !VerifyHash("super-secret", hash) {
		t.Fatal("correct raw secret must verify")
	}
	if VerifyHash("wrong", hash) {
		t.Fatal("wrong raw secret must not verify")
	}
	if VerifyHash("legacy", "legacy") != true {
		t.Fatal("legacy plaintext comparison must work")
	}
	if err := ValidateHash(hash); err != nil {
		t.Fatalf("ValidateHash failed: %v", err)
	}
	if err := ValidateHash("sha256:not-hex"); err == nil {
		t.Fatal("malformed hash must be rejected")
	}
}

func TestMapProviderConcurrentAccess(t *testing.T) {
	p := NewMapProvider(map[string]string{"A": "1"})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.SetMany(map[string]string{"A": "2"})
			_, _ = p.Get("A")
			_ = p.Snapshot()
		}()
	}
	wg.Wait()
}

func TestEnvFileProviderDeleteManyPreservesUnmanagedContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("# keep\nA=1\nB=2\nC=3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := NewEnvFileProvider(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteMany("B", "MISSING"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	if strings.Contains(text, "B=2") {
		t.Fatalf("B was not deleted:\n%s", text)
	}
	if !strings.Contains(text, "# keep") || !strings.Contains(text, "A=1") || !strings.Contains(text, "C=3") {
		t.Fatalf("unmanaged content was damaged:\n%s", text)
	}
	if _, err := p.Get("B"); err == nil {
		t.Fatal("deleted key still resolves from memory")
	}
}

func TestOpenEnvFileProviderDoesNotCreateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	p, err := OpenEnvFileProvider(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("OpenEnvFileProvider must not create a missing file")
	}
	if _, err := p.Get("A"); err == nil {
		t.Fatal("missing key must not resolve")
	}
	if err := p.SetMany(map[string]string{"A": "1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("SetMany must create the file")
	}
}
