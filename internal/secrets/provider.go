// Package secrets provides secret resolution and persistence for the gateway.
//
// Credentials are deliberately kept out of config.yaml. The YAML file only
// stores references to keys in a secret provider; this package resolves those
// references once during runtime construction, never on the request hot path.
package secrets

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// ErrNotFound is returned when a referenced secret does not exist.
var ErrNotFound = errors.New("secret not found")

// ErrStaleSnapshot is returned when a staged reload is committed after the
// provider's backing store changed. Callers should discard the candidate and
// let the next watcher event rebuild from the newer contents.
var ErrStaleSnapshot = errors.New("secret snapshot is stale")

// Provider resolves secret references by name.
//
// Implementations must be safe for concurrent use. The request path must never
// call a Provider directly: values are resolved while compiling an immutable
// runtime snapshot.
type Provider interface {
	// Get returns the value for a reference.
	Get(ref string) (string, error)

	// Snapshot returns an independent copy of all currently loaded values.
	Snapshot() map[string]string

	// SetMany atomically merges values into the provider's backing store.
	SetMany(values map[string]string) error

	// DeleteMany removes references from the provider's backing store.
	DeleteMany(refs ...string) error

	// Reload re-reads the backing store.
	Reload() error

	// Path identifies the backing store (empty for in-memory providers).
	Path() string
}

// Reloaded is a staged view of a provider's backing store. Values is safe to
// use for a candidate runtime build; Digest identifies the exact backing-store
// content so CommitReload can reject stale snapshots.
type Reloaded struct {
	Values map[string]string
	Digest [sha256.Size]byte
}

// Reloadable is implemented by providers that can load a candidate snapshot
// without publishing it. The runtime builder uses this to compile auth from a
// candidate while the currently published provider values remain untouched;
// CommitReload publishes the candidate only after the whole runtime built
// successfully.
type Reloadable interface {
	ReloadSnapshot() (Reloaded, error)
	CommitReload(Reloaded) error
}

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidateRef validates an environment-style secret reference name.
func ValidateRef(ref string) error {
	if ref == "" {
		return errors.New("secret reference is empty")
	}
	if !envKeyPattern.MatchString(ref) {
		return fmt.Errorf("secret reference %q is not a valid environment key", ref)
	}
	return nil
}

// validateValues rejects values that cannot be represented safely in an
// environment file. Newlines and NUL bytes would allow file-content injection.
func validateValues(values map[string]string) error {
	for ref, value := range values {
		if err := ValidateRef(ref); err != nil {
			return err
		}
		if strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("secret %q contains a newline or NUL byte", ref)
		}
	}
	return nil
}

func copyMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// MapProvider is an in-memory Provider. It is primarily useful for tests and
// for runtime construction where no persistent secret store is configured.
type MapProvider struct {
	mu     sync.RWMutex
	values map[string]string
}

// NewMapProvider creates an in-memory provider from a copy of values.
func NewMapProvider(values map[string]string) *MapProvider {
	return &MapProvider{values: copyMap(values)}
}

func (p *MapProvider) Get(ref string) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	value, ok := p.values[ref]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrNotFound, ref)
	}
	return value, nil
}

func (p *MapProvider) Snapshot() map[string]string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return copyMap(p.values)
}

// ReloadSnapshot returns a staged copy of the current in-memory values.
func (p *MapProvider) ReloadSnapshot() (Reloaded, error) {
	values := p.Snapshot()
	return Reloaded{Values: values, Digest: digestValues(values)}, nil
}

// CommitReload replaces the in-memory values only when the provider has not
// changed since ReloadSnapshot was called.
func (p *MapProvider) CommitReload(snapshot Reloaded) error {
	if err := validateValues(snapshot.Values); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if digestValues(p.values) != snapshot.Digest {
		return fmt.Errorf("%w: provider values changed", ErrStaleSnapshot)
	}
	p.values = copyMap(snapshot.Values)
	return nil
}

func (p *MapProvider) SetMany(values map[string]string) error {
	if err := validateValues(values); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.values == nil {
		p.values = make(map[string]string)
	}
	for ref, value := range values {
		p.values[ref] = value
	}
	return nil
}

func (p *MapProvider) DeleteMany(refs ...string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, ref := range refs {
		delete(p.values, ref)
	}
	return nil
}

func (p *MapProvider) Reload() error { return nil }

func (p *MapProvider) Path() string { return "" }

// Staged returns a read-only view of base with overrides applied. It is used
// to validate a candidate credential set before anything is persisted.
func Staged(base Provider, overrides map[string]string) Provider {
	return &overlayProvider{base: base, overrides: copyMap(overrides)}
}

type overlayProvider struct {
	base      Provider
	overrides map[string]string
}

func (p *overlayProvider) Get(ref string) (string, error) {
	if value, ok := p.overrides[ref]; ok {
		return value, nil
	}
	return p.base.Get(ref)
}

func (p *overlayProvider) Snapshot() map[string]string {
	values := p.base.Snapshot()
	if values == nil {
		values = make(map[string]string)
	}
	for ref, value := range p.overrides {
		values[ref] = value
	}
	return values
}

func (p *overlayProvider) SetMany(map[string]string) error {
	return errors.New("staged secret provider is read-only")
}

func (p *overlayProvider) DeleteMany(...string) error {
	return errors.New("staged secret provider is read-only")
}

func (p *overlayProvider) Reload() error { return p.base.Reload() }

func (p *overlayProvider) Path() string { return p.base.Path() }

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// digestValues produces a deterministic digest of a secret map. Length
// prefixes avoid ambiguity between keys and values containing separators.
func digestValues(values map[string]string) [sha256.Size]byte {
	hasher := sha256.New()
	var length [8]byte
	for _, key := range sortedKeys(values) {
		binary.BigEndian.PutUint64(length[:], uint64(len(key)))
		_, _ = hasher.Write(length[:])
		_, _ = hasher.Write([]byte(key))
		value := values[key]
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = hasher.Write(length[:])
		_, _ = hasher.Write([]byte(value))
	}
	var out [sha256.Size]byte
	copy(out[:], hasher.Sum(nil))
	return out
}

var _ Reloadable = (*MapProvider)(nil)
