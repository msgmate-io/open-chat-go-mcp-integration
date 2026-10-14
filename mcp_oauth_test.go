package mcpintegration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"backend/runtimecfg"
)

func TestResolveOAuthRedirectURIUsesPublicBaseURL(t *testing.T) {
	runtimecfg.SetAll(map[string]runtimecfg.Value{
		"PUBLIC_BASE_URL": {Value: "https://ci.msgmate.io/"},
	})
	t.Cleanup(func() { runtimecfg.SetAll(nil) })

	authCfg := map[string]interface{}{"redirect_uri": "http://localhost:1984/callback"}
	got := resolveOAuthRedirectURI(authCfg, httptest.NewRequest(http.MethodPost, "http://localhost:1984/x", nil))
	if got != "https://ci.msgmate.io/callback" {
		t.Fatalf("expected the public base url to replace the loopback redirect, got %q", got)
	}
}

func TestResolveOAuthRedirectURIPreservesCustomRedirect(t *testing.T) {
	runtimecfg.SetAll(map[string]runtimecfg.Value{
		"PUBLIC_BASE_URL": {Value: "https://ci.msgmate.io"},
	})
	t.Cleanup(func() { runtimecfg.SetAll(nil) })

	authCfg := map[string]interface{}{"redirect_uri": "https://oauth.example.com/callback"}
	got := resolveOAuthRedirectURI(authCfg, httptest.NewRequest(http.MethodPost, "http://localhost:1984/x", nil))
	if got != "https://oauth.example.com/callback" {
		t.Fatalf("expected a configured non-loopback redirect to be preserved, got %q", got)
	}
}

func TestResolveOAuthRedirectURIDerivesFromRequestHost(t *testing.T) {
	runtimecfg.SetAll(nil)
	t.Cleanup(func() { runtimecfg.SetAll(nil) })

	req := httptest.NewRequest(http.MethodPost, "http://backend:1984/x", nil)
	req.Header.Set("X-Forwarded-Host", "chat.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")

	got := resolveOAuthRedirectURI(map[string]interface{}{}, req)
	if got != "https://chat.example.com/callback" {
		t.Fatalf("expected the redirect derived from the forwarded host, got %q", got)
	}
}

func TestIsLoopbackRedirectURI(t *testing.T) {
	cases := map[string]bool{
		"":                               true,
		"http://localhost:1984/callback": true,
		"http://127.0.0.1:3000/callback": true,
		"http://[::1]:1984/callback":     true,
		"https://ci.msgmate.io/callback": false,
		"https://oauth.example.com/cb":   false,
	}
	for raw, want := range cases {
		if got := isLoopbackRedirectURI(raw); got != want {
			t.Fatalf("isLoopbackRedirectURI(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestOAuthAuthDataNeedsRefresh(t *testing.T) {
	fresh := map[string]interface{}{
		"refresh_token":    "refresh",
		"expires_in":       float64(3600),
		"obtained_at_unix": float64(time.Now().Unix()),
	}
	if oauthAuthDataNeedsRefresh(fresh) {
		t.Fatalf("expected a freshly obtained token to not need a refresh")
	}

	expired := map[string]interface{}{
		"refresh_token":    "refresh",
		"expires_in":       float64(3600),
		"obtained_at_unix": float64(time.Now().Add(-2 * time.Hour).Unix()),
	}
	if !oauthAuthDataNeedsRefresh(expired) {
		t.Fatalf("expected an expired token to need a refresh")
	}

	noRefresh := map[string]interface{}{
		"expires_in":       float64(3600),
		"obtained_at_unix": float64(time.Now().Add(-2 * time.Hour).Unix()),
	}
	if oauthAuthDataNeedsRefresh(noRefresh) {
		t.Fatalf("expected a token without a refresh_token to not be refreshed")
	}

	noExpiry := map[string]interface{}{"refresh_token": "refresh"}
	if oauthAuthDataNeedsRefresh(noExpiry) {
		t.Fatalf("expected a token without expires_in to be treated as non-expiring")
	}
}

func TestOAuthTokenRefresh(t *testing.T) {
	var gotForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "new-access",
			"token_type":   "Bearer",
			"expires_in":   1800,
		})
	}))
	defer server.Close()

	authCfg := map[string]interface{}{
		"token_url": server.URL,
		"client_id": "client-id",
	}
	authData := map[string]interface{}{
		"refresh_token":    "old-refresh",
		"obtained_at_unix": float64(time.Now().Add(-2 * time.Hour).Unix()),
	}

	refreshed, err := oauthTokenRefresh(authCfg, authData)
	if err != nil {
		t.Fatalf("oauthTokenRefresh failed: %v", err)
	}
	if got := refreshed["access_token"]; got != "new-access" {
		t.Fatalf("expected the refreshed access token, got %v", got)
	}
	if got := refreshed["refresh_token"]; got != "old-refresh" {
		t.Fatalf("expected the previous refresh token to be preserved, got %v", got)
	}
	if _, ok := refreshed["obtained_at_unix"]; !ok {
		t.Fatalf("expected obtained_at_unix to be set on the refreshed token")
	}
	if got := gotForm.Get("grant_type"); got != "refresh_token" {
		t.Fatalf("expected a refresh_token grant, got %q", got)
	}
	if got := gotForm.Get("refresh_token"); got != "old-refresh" {
		t.Fatalf("expected the stored refresh token to be sent, got %q", got)
	}
	if got := gotForm.Get("client_id"); got != "client-id" {
		t.Fatalf("expected the client id to be sent, got %q", got)
	}
	if strings.TrimSpace(refreshed["client_id"].(string)) != "client-id" {
		t.Fatalf("expected the client id to be persisted for future refreshes")
	}
}
