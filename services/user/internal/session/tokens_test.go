package session_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"typefast/services/user/internal/apperr"
	"typefast/services/user/internal/session"
)

func newManager(t *testing.T, audience string) *session.TokenManager {
	t.Helper()
	manager, _ := newManagerWithKey(t, audience, 15*time.Minute)
	return manager
}

// newManagerWithKey exposes the signing key so tests can forge tokens the
// issuer itself would never produce.
func newManagerWithKey(t *testing.T, audience string, ttl time.Duration) (*session.TokenManager, ed25519.PrivateKey) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := session.NewTokenManager(privateKey, "test-key", "kit-user", audience, ttl)
	if err != nil {
		t.Fatal(err)
	}
	return manager, privateKey
}

func forgeToken(t *testing.T, privateKey ed25519.PrivateKey, claims session.Claims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = "test-key"
	signed, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func validClaims() session.Claims {
	now := time.Now().UTC()
	return session.Claims{
		SessionID: uuid.Must(uuid.NewV7()).String(),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "kit-user",
			Subject:   uuid.Must(uuid.NewV7()).String(),
			Audience:  jwt.ClaimStrings{"kit-api"},
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
}

func TestTokenManagerIssueAndParse(t *testing.T) {
	t.Parallel()

	manager := newManager(t, "kit-api")
	userID := uuid.Must(uuid.NewV7())
	sessionID := uuid.Must(uuid.NewV7())

	raw, expiresAt, err := manager.Issue(userID, sessionID, []string{"user"}, []string{"account.read"})
	if err != nil {
		t.Fatal(err)
	}
	if raw == "" {
		t.Fatal("empty token")
	}
	if time.Until(expiresAt) <= 0 {
		t.Fatal("token is already expired")
	}

	claims, err := manager.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != userID.String() {
		t.Fatalf("subject = %q", claims.Subject)
	}
	if claims.SessionID != sessionID.String() {
		t.Fatalf("session = %q", claims.SessionID)
	}
	if len(claims.Permissions) != 1 || claims.Permissions[0] != "account.read" {
		t.Fatalf("permissions = %v", claims.Permissions)
	}
}

func TestTokenManagerRejectsTampering(t *testing.T) {
	t.Parallel()

	manager := newManager(t, "kit-api")
	raw, _, err := manager.Issue(uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	tampered := raw[:len(raw)-2] + "aa"
	if _, err := manager.Parse(tampered); !apperr.IsKind(err, apperr.KindUnauthorized) {
		t.Fatalf("expected unauthorized, got %v", err)
	}
}

func TestTokenManagerRejectsWrongAudience(t *testing.T) {
	t.Parallel()

	issuer := newManager(t, "kit-api")
	verifier := newManager(t, "other-api")

	raw, _, err := issuer.Issue(uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Parse(raw); err == nil {
		t.Fatal("expected wrong audience to fail")
	}
}

func TestTokenManagerRejectsWrongKey(t *testing.T) {
	t.Parallel()

	issuer := newManager(t, "kit-api")
	verifier := newManager(t, "kit-api")

	raw, _, err := issuer.Issue(uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Parse(raw); err == nil {
		t.Fatal("expected token signed by another key to fail")
	}
}

// A correctly signed token must still be refused once it has expired, and the
// issuer must never accept a token without an expiry at all.
func TestTokenManagerRejectsExpiredAndUnboundedTokens(t *testing.T) {
	t.Parallel()

	manager, privateKey := newManagerWithKey(t, "kit-api", time.Minute)

	expired := validClaims()
	expired.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
	if _, err := manager.Parse(forgeToken(t, privateKey, expired)); !apperr.IsKind(err, apperr.KindUnauthorized) {
		t.Fatalf("expected expired token to be unauthorized, got %v", err)
	}

	unbounded := validClaims()
	unbounded.ExpiresAt = nil
	if _, err := manager.Parse(forgeToken(t, privateKey, unbounded)); !apperr.IsKind(err, apperr.KindUnauthorized) {
		t.Fatalf("expected token without expiry to be unauthorized, got %v", err)
	}

	if _, err := manager.Parse(forgeToken(t, privateKey, validClaims())); err != nil {
		t.Fatalf("expected a matching forged token to parse: %v", err)
	}
}

func TestNewTokenManagerValidation(t *testing.T) {
	t.Parallel()

	if _, err := session.NewTokenManager(nil, "key", "issuer", "audience", time.Minute); err == nil {
		t.Fatal("expected nil key to fail")
	}

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.NewTokenManager(privateKey, "key", "issuer", "audience", 0); err == nil {
		t.Fatal("expected zero TTL to fail")
	}
}
