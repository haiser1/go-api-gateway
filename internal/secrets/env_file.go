package secrets

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// EnvFileEnvVar allows overriding the default environment-file location.
const EnvFileEnvVar = "GATEWAY_ENV_FILE"

// DefaultPath resolves the environment file location:
//
//  1. $GATEWAY_ENV_FILE, if set;
//  2. configs/.env, when a configs directory exists relative to the working
//     directory (the same directory that holds config.yaml);
//  3. .env in the working directory as a fallback.
func DefaultPath() string {
	if p := os.Getenv(EnvFileEnvVar); p != "" {
		return p
	}
	if info, err := os.Stat("configs"); err == nil && info.IsDir() {
		return filepath.Join("configs", ".env")
	}
	return ".env"
}

// EnvFileProvider loads and persists secrets in a dotenv-style file.
//
// Values are loaded once at construction and refreshed only by Reload or
// SetMany. Requests never read the file.
type EnvFileProvider struct {
	path string

	mu     sync.RWMutex
	values map[string]string
}

var _ Reloadable = (*EnvFileProvider)(nil)

// NewEnvFileProvider creates a provider backed by path. The parent directory
// is created with 0700 permissions and the file with 0600 permissions when
// missing. Existing files are read but never rewritten by construction.
func NewEnvFileProvider(path string) (*EnvFileProvider, error) {
	if path == "" {
		path = DefaultPath()
	}

	if err := ensureSecretDir(filepath.Dir(path)); err != nil {
		return nil, err
	}

	if _, err := os.Stat(path); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("failed to stat env file: %w", err)
		}
		if err := createEmptyEnvFile(path); err != nil {
			return nil, err
		}
	}

	values, err := LoadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load env file: %w", err)
	}

	return &EnvFileProvider{path: path, values: values}, nil
}

// OpenEnvFileProvider loads path without creating it when missing. Writes
// through SetMany still create the directory and file with secure
// permissions. Use this when the gateway should not materialize an empty
// secret file merely because auth is not configured yet.
func OpenEnvFileProvider(path string) (*EnvFileProvider, error) {
	if path == "" {
		path = DefaultPath()
	}
	provider := &EnvFileProvider{path: path, values: make(map[string]string)}
	if err := provider.Reload(); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return provider, nil
		}
		return nil, err
	}
	return provider, nil
}

func ensureSecretDir(dir string) error {
	if dir == "" || dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create secret directory: %w", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("failed to stat secret directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("secret path parent %q is not a directory", dir)
	}
	if info.Mode().Perm() != 0o700 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("failed to restrict secret directory permissions: %w", err)
		}
	}
	return nil
}

func createEmptyEnvFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil
		}
		return fmt.Errorf("failed to create env file: %w", err)
	}
	_, writeErr := f.WriteString("# Gateway secrets - do NOT commit this file\n")
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("failed to initialize env file: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("failed to close env file: %w", closeErr)
	}
	return nil
}

// LoadFile parses a dotenv-style file. Comments (full-line or inline after a
// value), blank lines, optional single/double quotes and an optional "export"
// prefix are supported. Values referencing other keys are NOT expanded.
func LoadFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(string(data)), nil
}

// Parse parses dotenv-style content into a map.
func Parse(content string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		trimmed = strings.TrimPrefix(trimmed, "export ")
		idx := strings.Index(trimmed, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		val := strings.TrimSpace(trimmed[idx+1:])

		if i := strings.Index(val, " #"); i >= 0 {
			val = strings.TrimSpace(val[:i])
		}
		out[key] = unquote(val)
	}
	return out
}

func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

func (p *EnvFileProvider) Get(ref string) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	value, ok := p.values[ref]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrNotFound, ref)
	}
	return value, nil
}

func (p *EnvFileProvider) Snapshot() map[string]string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return copyMap(p.values)
}

// ReloadSnapshot reads and validates the env file without publishing the
// result. The returned digest identifies the exact file contents, so a
// concurrent write (or an atomic replacement by the admin API) makes the
// subsequent CommitReload fail instead of publishing stale secrets.
func (p *EnvFileProvider) ReloadSnapshot() (Reloaded, error) {
	data, err := os.ReadFile(p.path)
	if err != nil {
		return Reloaded{}, fmt.Errorf("failed to reload env file: %w", err)
	}
	values := Parse(string(data))
	if err := validateValues(values); err != nil {
		return Reloaded{}, fmt.Errorf("failed to reload env file: %w", err)
	}
	return Reloaded{Values: values, Digest: sha256.Sum256(data)}, nil
}

// CommitReload publishes a previously staged env-file snapshot, but only if
// the file still has exactly the contents that were staged.
func (p *EnvFileProvider) CommitReload(snapshot Reloaded) error {
	if err := validateValues(snapshot.Values); err != nil {
		return err
	}

	// Hold the provider lock across the digest check and publication so a
	// concurrent SetMany/DeleteMany cannot land between them and be
	// overwritten by this staged snapshot.
	p.mu.Lock()
	defer p.mu.Unlock()

	data, err := os.ReadFile(p.path)
	if err != nil {
		return fmt.Errorf("failed to reload env file: %w", err)
	}
	if sha256.Sum256(data) != snapshot.Digest {
		return fmt.Errorf("%w: env file changed during runtime build", ErrStaleSnapshot)
	}

	p.values = copyMap(snapshot.Values)
	return nil
}

func (p *EnvFileProvider) Reload() error {
	// A concurrent atomic replacement can land between the snapshot read and
	// the commit digest check. Retry a few times; a persistent writer will
	// still surface ErrStaleSnapshot instead of publishing a torn view.
	for attempt := 0; attempt < 3; attempt++ {
		snapshot, err := p.ReloadSnapshot()
		if err != nil {
			return err
		}
		err = p.CommitReload(snapshot)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrStaleSnapshot) {
			return err
		}
	}
	return fmt.Errorf("%w: env file kept changing during reload", ErrStaleSnapshot)
}

func (p *EnvFileProvider) SetMany(values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	if err := validateValues(values); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	original, err := os.ReadFile(p.path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to read env file: %w", err)
	}
	rendered := renderEnv(string(original), values)
	if err := writeFileAtomic(p.path, []byte(rendered)); err != nil {
		return err
	}

	merged := copyMap(p.values)
	if merged == nil {
		merged = make(map[string]string)
	}
	for ref, value := range values {
		merged[ref] = value
	}
	p.values = merged
	return nil
}

func (p *EnvFileProvider) DeleteMany(refs ...string) error {
	if len(refs) == 0 {
		return nil
	}
	remove := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if err := ValidateRef(ref); err != nil {
			return err
		}
		remove[ref] = struct{}{}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	original, err := os.ReadFile(p.path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to read env file: %w", err)
	}
	if err == nil {
		rendered := deleteEnvKeys(string(original), remove)
		if err := writeFileAtomic(p.path, []byte(rendered)); err != nil {
			return err
		}
	}

	merged := copyMap(p.values)
	if merged == nil {
		merged = make(map[string]string)
	}
	for ref := range remove {
		delete(merged, ref)
	}
	p.values = merged
	return nil
}

func (p *EnvFileProvider) Path() string { return p.path }

func deleteEnvKeys(content string, remove map[string]struct{}) string {
	if content == "" {
		return content
	}
	trailingNewline := strings.HasSuffix(content, "\n")
	lines := strings.Split(content, "\n")
	if trailingNewline {
		lines = lines[:len(lines)-1]
	}
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if key, ok := envLineKey(line); ok {
			if _, drop := remove[key]; drop {
				continue
			}
		}
		kept = append(kept, line)
	}
	result := strings.Join(kept, "\n")
	if trailingNewline && result != "" {
		result += "\n"
	}
	return result
}

func envLineKey(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	trimmed = strings.TrimPrefix(trimmed, "export ")
	idx := strings.Index(trimmed, "=")
	if idx <= 0 {
		return "", false
	}
	return strings.TrimSpace(trimmed[:idx]), true
}

// renderEnv replaces managed keys in place while preserving comments, blank
// lines, ordering, and unrelated variables. Missing keys are appended.
func renderEnv(original string, updates map[string]string) string {
	if original == "" {
		return renderNewEnv(updates)
	}

	lines := strings.Split(original, "\n")
	written := make(map[string]bool, len(updates))
	out := make([]string, 0, len(lines)+len(updates))

	for _, line := range lines {
		if key, ok := envLineKey(line); ok {
			if value, found := updates[key]; found {
				out = append(out, key+"="+value)
				written[key] = true
				continue
			}
		}
		out = append(out, line)
	}

	missing := make([]string, 0, len(updates))
	for key := range updates {
		if !written[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)

	// If the file ended with a newline, Split produced a trailing empty
	// element. Insert missing keys before it so the final newline survives.
	trailingEmpty := len(out) > 0 && out[len(out)-1] == ""
	if trailingEmpty {
		out = out[:len(out)-1]
	}
	for _, key := range missing {
		out = append(out, key+"="+updates[key])
	}
	result := strings.Join(out, "\n")
	if trailingEmpty || len(missing) > 0 {
		result += "\n"
	}
	return result
}

func renderNewEnv(updates map[string]string) string {
	keys := sortedKeys(updates)
	var b strings.Builder
	b.WriteString("# Gateway secrets - do NOT commit this file\n")
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(updates[key])
		b.WriteByte('\n')
	}
	return b.String()
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := ensureSecretDir(dir); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".env.tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp env file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to set temp env permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write temp env file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to flush temp env file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp env file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to replace env file: %w", err)
	}
	cleanup = false

	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("failed to restrict env file permissions: %w", err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
