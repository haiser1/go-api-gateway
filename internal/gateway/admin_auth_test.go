package gateway

import (
	"testing"

	"github.com/haiser1/go-api-gateway/internal/secrets"
)

func TestLoadAdminAuthenticator(t *testing.T) {
	t.Setenv("GATEWAY_ADMIN_TOKEN", "env-token")
	authenticator, err := loadAdminAuthenticator(secrets.NewMapProvider(nil))
	if err != nil || authenticator == nil || !authenticator.Verify("env-token") {
		t.Fatalf("env admin token not loaded: %v", err)
	}

	t.Setenv("GATEWAY_ADMIN_TOKEN", "")
	authenticator, err = loadAdminAuthenticator(secrets.NewMapProvider(map[string]string{
		secrets.AdminTokenHashKey: secrets.HashSecret("hashed-token"),
	}))
	if err != nil || authenticator == nil || !authenticator.Verify("hashed-token") {
		t.Fatalf("hashed admin token not loaded: %v", err)
	}

	authenticator, err = loadAdminAuthenticator(secrets.NewMapProvider(nil))
	if err != nil || authenticator != nil {
		t.Fatalf("missing admin token must yield nil authenticator: %v", err)
	}

	if _, err := loadAdminAuthenticator(secrets.NewMapProvider(map[string]string{
		secrets.AdminTokenHashKey: "sha256:bad",
	})); err == nil {
		t.Fatal("malformed admin hash must fail")
	}
}
