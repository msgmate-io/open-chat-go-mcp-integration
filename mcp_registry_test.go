package mcpintegration

import (
	"testing"

	"backend/database"
	"backend/runtimecfg"
)

func TestBuiltinMCPServerSpecPlaywright(t *testing.T) {
	spec, ok := BuiltinMCPServerSpec("playwright")
	if !ok {
		t.Fatalf("expected the playwright preset to resolve")
	}
	if spec.Name != MCPPresetPlaywright {
		t.Fatalf("expected preset name %q, got %q", MCPPresetPlaywright, spec.Name)
	}
	if !spec.Enabled {
		t.Fatalf("expected the built-in preset to be enabled")
	}
	if spec.Transport != MCPServerTransportStdio {
		t.Fatalf("expected stdio transport, got %q", spec.Transport)
	}
	if spec.Command != "npx" {
		t.Fatalf("expected npx command, got %q", spec.Command)
	}
	if len(spec.Args) == 0 || spec.Args[0] != "-y" {
		t.Fatalf("expected the preset args to start with -y, got %v", spec.Args)
	}
	if spec.TimeoutMS <= 0 {
		t.Fatalf("expected a positive tool-discovery timeout, got %d", spec.TimeoutMS)
	}

	if _, ok := BuiltinMCPServerSpec("does-not-exist"); ok {
		t.Fatalf("expected an unknown preset to be rejected")
	}
}

func TestRegisterResolveAndUnregisterMCPServerSpec(t *testing.T) {
	db := setupMCPIntegrationTestDB(t)
	owner := createUserForMCPIntegrationTest(t, db, "registry-owner@example.com")
	runtimecfg.SetAll(nil)
	t.Cleanup(func() { runtimecfg.SetAll(nil) })

	registered, err := RegisterMCPServer(db, owner.ID, MCPServerSpec{
		Name:      "playwright-remote",
		Enabled:   true,
		Transport: MCPServerTransportHTTPStreamable,
		URL:       "http://dev-sandbox:8931/mcp",
		AuthMode:  "bearer_token",
		AuthData:  map[string]interface{}{"bearer_token": "secret-token"},
	})
	if err != nil {
		t.Fatalf("RegisterMCPServer failed: %v", err)
	}
	if registered.Transport != MCPServerTransportHTTPStreamable {
		t.Fatalf("expected http_streamable transport, got %q", registered.Transport)
	}
	if registered.URL != "http://dev-sandbox:8931/mcp" {
		t.Fatalf("expected url to round-trip, got %q", registered.URL)
	}
	if registered.AuthMode != "bearer_token" {
		t.Fatalf("expected bearer_token auth mode, got %q", registered.AuthMode)
	}
	if got, _ := registered.AuthData["bearer_token"].(string); got != "secret-token" {
		t.Fatalf("expected auth data to round-trip, got %v", registered.AuthData)
	}

	resolved, err := ResolveMCPServerSpec(db, owner.ID, "playwright-remote")
	if err != nil {
		t.Fatalf("ResolveMCPServerSpec failed: %v", err)
	}
	if resolved.URL != "http://dev-sandbox:8931/mcp" {
		t.Fatalf("expected resolved url, got %q", resolved.URL)
	}

	specs, err := ResolveMCPServerSpecs(db, owner.ID, []string{"playwright-remote"})
	if err != nil {
		t.Fatalf("ResolveMCPServerSpecs failed: %v", err)
	}
	if len(specs) != 1 || specs[0].Name != "playwright-remote" {
		t.Fatalf("expected the filtered server, got %+v", specs)
	}

	// Registering again with the same name updates in place (no duplicate).
	if _, err := RegisterMCPServer(db, owner.ID, MCPServerSpec{
		Name:      "playwright-remote",
		Enabled:   true,
		Transport: MCPServerTransportHTTPStreamable,
		URL:       "http://other-host:8931/mcp",
		AuthMode:  "none",
	}); err != nil {
		t.Fatalf("second RegisterMCPServer failed: %v", err)
	}
	var count int64
	if err := db.Model(&database.MCPIntegrationConfig{}).
		Where("owner_user_id = ? AND name = ?", owner.ID, "playwright-remote").
		Count(&count).Error; err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one row after upsert, got %d", count)
	}
	resolved, err = ResolveMCPServerSpec(db, owner.ID, "playwright-remote")
	if err != nil {
		t.Fatalf("ResolveMCPServerSpec after update failed: %v", err)
	}
	if resolved.URL != "http://other-host:8931/mcp" {
		t.Fatalf("expected updated url, got %q", resolved.URL)
	}

	if err := UnregisterMCPServer(db, owner.ID, "playwright-remote"); err != nil {
		t.Fatalf("UnregisterMCPServer failed: %v", err)
	}
	if _, err := ResolveMCPServerSpec(db, owner.ID, "playwright-remote"); err == nil {
		t.Fatalf("expected the server to be gone after unregister")
	}
}

func TestResolveMCPServerSpecsSkipsDisabled(t *testing.T) {
	db := setupMCPIntegrationTestDB(t)
	owner := createUserForMCPIntegrationTest(t, db, "registry-disabled@example.com")
	runtimecfg.SetAll(nil)
	t.Cleanup(func() { runtimecfg.SetAll(nil) })

	if _, err := RegisterMCPServer(db, owner.ID, MCPServerSpec{
		Name:      "off-server",
		Enabled:   false,
		Transport: MCPServerTransportHTTPStreamable,
		URL:       "http://off.example.com/mcp",
		AuthMode:  "none",
	}); err != nil {
		t.Fatalf("RegisterMCPServer failed: %v", err)
	}
	if _, err := RegisterMCPServer(db, owner.ID, MCPServerSpec{
		Name:      "on-server",
		Enabled:   true,
		Transport: MCPServerTransportHTTPStreamable,
		URL:       "http://on.example.com/mcp",
		AuthMode:  "none",
	}); err != nil {
		t.Fatalf("RegisterMCPServer failed: %v", err)
	}

	specs, err := ResolveMCPServerSpecs(db, owner.ID, nil)
	if err != nil {
		t.Fatalf("ResolveMCPServerSpecs failed: %v", err)
	}
	if len(specs) != 1 || specs[0].Name != "on-server" {
		t.Fatalf("expected only the enabled server, got %+v", specs)
	}
}
