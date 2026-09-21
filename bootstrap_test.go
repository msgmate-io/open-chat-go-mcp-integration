package mcpintegration

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"backend/database"
	"backend/runtimecfg"
	"backend/server/util"
	"gorm.io/gorm"
)

func setupMCPIntegrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	cfg := database.DBConfig{
		Backend:  "sqlite",
		FilePath: filepath.Join(t.TempDir(), "mcp_integration_test.db"),
		Debug:    false,
		ResetDB:  true,
	}
	db := database.SetupDatabase(cfg)
	if err := db.AutoMigrate(&database.MCPIntegrationConfig{}); err != nil {
		t.Fatalf("auto-migrate mcp models failed: %v", err)
	}
	return db
}

func createUserForMCPIntegrationTest(t *testing.T, db *gorm.DB, email string) *database.User {
	t.Helper()
	err, user := util.CreateUser(db, email, "Passw0rd!", false)
	if err != nil {
		t.Fatalf("failed to create user %q: %v", email, err)
	}
	return user
}

func loadConfigMap(t *testing.T, raw json.RawMessage) map[string]interface{} {
	t.Helper()
	out := map[string]interface{}{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("failed to decode config: %v", err)
	}
	return out
}

func TestApplyBootstrapTemplateMergeAndPartialAuth(t *testing.T) {
	db := setupMCPIntegrationTestDB(t)
	owner := createUserForMCPIntegrationTest(t, db, "admin@example.com")
	runtimecfg.SetAll(nil)
	t.Cleanup(func() { runtimecfg.SetAll(nil) })

	result, err := ApplyBootstrap(db, BootstrapSpec{
		FallbackOwner: owner.Username,
		Servers: []BootstrapServerSpec{
			{
				Name:     "google-drive",
				Template: "google_workspace_drive",
				Config: map[string]interface{}{
					"auth": map[string]interface{}{
						"client_id":     "123-abc.apps.googleusercontent.com",
						"client_secret": "GOCSPX-secret",
						"redirect_uri":  "https://chat.example.com/callback",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	if result.ServersCreated != 1 {
		t.Fatalf("unexpected counts: %+v", result)
	}

	row, err := loadOwnedServer(db, owner.ID, "google-drive")
	if err != nil {
		t.Fatalf("seeded row missing: %v", err)
	}
	if !row.Enabled {
		t.Fatalf("server should default to enabled")
	}
	config := loadConfigMap(t, row.Config)
	if config["url"] != "https://drivemcp.googleapis.com/mcp/v1" {
		t.Fatalf("template url not merged: %+v", config)
	}
	auth, _ := config["auth"].(map[string]interface{})
	if auth["client_id"] != "123-abc.apps.googleusercontent.com" {
		t.Fatalf("client_id override not applied: %+v", auth)
	}
	if auth["client_secret"] != "GOCSPX-secret" {
		t.Fatalf("client_secret override not applied: %+v", auth)
	}
	if auth["redirect_uri"] != "https://chat.example.com/callback" {
		t.Fatalf("redirect_uri override not applied: %+v", auth)
	}
	if auth["authorize_url"] == "" || auth["token_url"] == "" {
		t.Fatalf("template oauth endpoints missing after merge: %+v", auth)
	}
	if _, ok := auth["scopes"]; !ok {
		t.Fatalf("template scopes missing after merge: %+v", auth)
	}
}

func TestApplyBootstrapSheetsTemplatePartialAuth(t *testing.T) {
	db := setupMCPIntegrationTestDB(t)
	owner := createUserForMCPIntegrationTest(t, db, "admin-sheets@example.com")

	result, err := ApplyBootstrap(db, BootstrapSpec{
		FallbackOwner: owner.Username,
		Servers: []BootstrapServerSpec{
			{
				Name:     "google-sheets",
				Template: "google_workspace_sheets",
				Config: map[string]interface{}{
					"auth": map[string]interface{}{"client_id": "only-client-id"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	if result.ServersCreated != 1 {
		t.Fatalf("unexpected counts: %+v", result)
	}
	row, err := loadOwnedServer(db, owner.ID, "google-sheets")
	if err != nil {
		t.Fatalf("seeded row missing: %v", err)
	}
	config := loadConfigMap(t, row.Config)
	if config["url"] != "https://sheetsmcp.googleapis.com/mcp/v1" {
		t.Fatalf("sheets template url not merged: %+v", config)
	}
	auth, _ := config["auth"].(map[string]interface{})
	if auth["client_id"] != "only-client-id" || auth["token_url"] == "" {
		t.Fatalf("partial auth not accepted / template endpoints missing: %+v", auth)
	}
}

func TestApplyBootstrapPlainNoAuthServer(t *testing.T) {
	db := setupMCPIntegrationTestDB(t)
	owner := createUserForMCPIntegrationTest(t, db, "admin-plain@example.com")

	result, err := ApplyBootstrap(db, BootstrapSpec{
		FallbackOwner: owner.Username,
		Servers: []BootstrapServerSpec{
			{Name: "playwright", Template: "playwright"},
		},
	})
	if err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	if result.ServersCreated != 1 {
		t.Fatalf("unexpected counts: %+v", result)
	}
}

func TestApplyBootstrapErrors(t *testing.T) {
	db := setupMCPIntegrationTestDB(t)
	owner := createUserForMCPIntegrationTest(t, db, "admin-errors@example.com")

	if _, err := ApplyBootstrap(db, BootstrapSpec{
		FallbackOwner: owner.Username,
		Servers:       []BootstrapServerSpec{{Name: "bad", Template: "does_not_exist"}},
	}); err == nil {
		t.Fatalf("expected unknown template error")
	}

	if _, err := ApplyBootstrap(db, BootstrapSpec{
		FallbackOwner: "nobody@example.com",
		Servers:       []BootstrapServerSpec{{Name: "plain", Config: map[string]interface{}{"url": "https://example.com/mcp"}}},
	}); err == nil {
		t.Fatalf("expected unknown owner error")
	}

	if _, err := ApplyBootstrap(db, BootstrapSpec{
		FallbackOwner: owner.Username,
		Servers:       []BootstrapServerSpec{{Name: "no-config"}},
	}); err == nil {
		t.Fatalf("expected missing config error")
	}
}

func TestApplyBootstrapIdempotentAndOverwrite(t *testing.T) {
	db := setupMCPIntegrationTestDB(t)
	owner := createUserForMCPIntegrationTest(t, db, "admin-idempotent@example.com")

	spec := func(overwrite bool, clientID string) BootstrapSpec {
		return BootstrapSpec{
			FallbackOwner: owner.Username,
			Servers: []BootstrapServerSpec{{
				Name:      "google-drive",
				Template:  "google_workspace_drive",
				Overwrite: overwrite,
				Config: map[string]interface{}{
					"auth": map[string]interface{}{"client_id": clientID},
				},
			}},
		}
	}

	if _, err := ApplyBootstrap(db, spec(false, "client-one")); err != nil {
		t.Fatalf("first bootstrap failed: %v", err)
	}
	first, err := loadOwnedServer(db, owner.ID, "google-drive")
	if err != nil {
		t.Fatalf("seeded row missing: %v", err)
	}

	// Simulate a UI-completed OAuth token that must survive a restart.
	tokenJSON := json.RawMessage(`{"access_token":"token-123","token_type":"Bearer"}`)
	if err := db.Model(&first).Update("auth_data", tokenJSON).Error; err != nil {
		t.Fatalf("failed to seed auth data: %v", err)
	}

	// Second apply without overwrite must skip and preserve existing data.
	result, err := ApplyBootstrap(db, spec(false, "client-two"))
	if err != nil {
		t.Fatalf("second bootstrap failed: %v", err)
	}
	if result.ServersSkipped != 1 || result.ServersUpdated != 0 {
		t.Fatalf("expected skip, got: %+v", result)
	}
	skipped, err := loadOwnedServer(db, owner.ID, "google-drive")
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	config := loadConfigMap(t, skipped.Config)
	auth, _ := config["auth"].(map[string]interface{})
	if auth["client_id"] != "client-one" {
		t.Fatalf("config should not be overwritten on skip: %+v", auth)
	}
	if string(skipped.AuthData) != string(tokenJSON) {
		t.Fatalf("auth_data was wiped on skip: %s", string(skipped.AuthData))
	}

	// Overwrite without auth_data updates config but leaves tokens intact.
	result, err = ApplyBootstrap(db, spec(true, "client-two"))
	if err != nil {
		t.Fatalf("overwrite bootstrap failed: %v", err)
	}
	if result.ServersUpdated != 1 {
		t.Fatalf("expected update, got: %+v", result)
	}
	updated, err := loadOwnedServer(db, owner.ID, "google-drive")
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	config = loadConfigMap(t, updated.Config)
	auth, _ = config["auth"].(map[string]interface{})
	if auth["client_id"] != "client-two" {
		t.Fatalf("overwrite did not update config: %+v", auth)
	}
	if string(updated.AuthData) != string(tokenJSON) {
		t.Fatalf("overwrite without auth_data wiped tokens: %s", string(updated.AuthData))
	}
}

func TestApplyRuntimeConfigBootstrapFromEnvValues(t *testing.T) {
	db := setupMCPIntegrationTestDB(t)
	owner := createUserForMCPIntegrationTest(t, db, "admin-runtime@example.com")

	runtimecfg.SetAll(map[string]runtimecfg.Value{
		bootstrapServersEnvKey: {Value: `{"name":"cfg-drive","template":"google_workspace_drive","config":{"auth":{"client_id":"env-client"}}}`},
	})
	t.Cleanup(func() { runtimecfg.SetAll(nil) })

	result, err := ApplyRuntimeConfigBootstrap(db, owner.Username)
	if err != nil {
		t.Fatalf("runtime config bootstrap failed: %v", err)
	}
	if result.ServersCreated != 1 {
		t.Fatalf("unexpected counts: %+v", result)
	}
	row, err := loadOwnedServer(db, owner.ID, "cfg-drive")
	if err != nil {
		t.Fatalf("env-seeded row missing: %v", err)
	}
	config := loadConfigMap(t, row.Config)
	auth, _ := config["auth"].(map[string]interface{})
	if auth["client_id"] != "env-client" {
		t.Fatalf("env spec not applied: %+v", auth)
	}

	// empty env -> no-op, no error
	runtimecfg.SetAll(map[string]runtimecfg.Value{})
	result, err = ApplyRuntimeConfigBootstrap(db, owner.Username)
	if err != nil {
		t.Fatalf("empty runtime config bootstrap failed: %v", err)
	}
	if result.ServersCreated != 0 || result.ServersUpdated != 0 || result.ServersSkipped != 0 {
		t.Fatalf("expected no-op, got: %+v", result)
	}
}