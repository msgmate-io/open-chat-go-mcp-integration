package mcpintegration

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"backend/database"

	"gorm.io/gorm"
)

// MCP server transport kinds understood by the registry. Consumer integrations
// map these to their own runtime config format.
const (
	MCPServerTransportHTTPStreamable = "http_streamable"
	MCPServerTransportSSE            = "sse"
	MCPServerTransportStdio          = "stdio"
)

// Well-known built-in MCP server presets that consumers (eg the opencode
// integration) can enable without a separately registered row.
const (
	MCPPresetPlaywright = "playwright"
)

// MCPServerSpec is the transport-neutral description of one owner-scoped MCP
// server. It is the shared abstraction other integrations consume to attach an
// owner's MCP servers to their own runtime configuration without depending on
// the MCP integration's storage layout.
//
// Consumers render the spec into their own config (eg the opencode integration
// maps remote transports to its `remote` MCP entries and stdio transports to
// `local` entries).
type MCPServerSpec struct {
	Name      string                 `json:"name"`
	Enabled   bool                   `json:"enabled"`
	Transport string                 `json:"transport"`
	URL       string                 `json:"url,omitempty"`
	Command   string                 `json:"command,omitempty"`
	Args      []string               `json:"args,omitempty"`
	Env       map[string]string      `json:"env,omitempty"`
	Headers   map[string]string      `json:"headers,omitempty"`
	AuthMode  string                 `json:"auth_mode,omitempty"`
	AuthData  map[string]interface{} `json:"auth_data,omitempty"`
	Config    map[string]interface{} `json:"config,omitempty"`
	// TimeoutMS bounds how long a consumer waits for the server to answer a
	// tools/list request. Zero leaves the consumer's default in place.
	TimeoutMS int `json:"timeout_ms,omitempty"`
}

// normalizeMCPServerTransport maps the stored/declared transport to a canonical
// kind. Empty input is returned unchanged so callers can fall back to inferring
// the transport from the presence of a URL or command.
func normalizeMCPServerTransport(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "none":
		return ""
	case "http", "https", "http_stream", "http-stream", "httpstream", "streamable_http", "streamable-http", "http_streamable":
		return MCPServerTransportHTTPStreamable
	case "sse", "server_sent_events", "server-sent-events":
		return MCPServerTransportSSE
	case "stdio", "stdin", "process", "local":
		return MCPServerTransportStdio
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func stringFromConfigMap(config map[string]interface{}, key string) string {
	if config == nil {
		return ""
	}
	v, _ := config[key].(string)
	return strings.TrimSpace(v)
}

func stringSliceFromConfigMap(config map[string]interface{}, key string) []string {
	if config == nil {
		return nil
	}
	switch raw := config[key].(type) {
	case []string:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	default:
		return nil
	}
}

func intFromConfigMap(config map[string]interface{}, key string) int {
	if config == nil {
		return 0
	}
	switch raw := config[key].(type) {
	case int:
		return raw
	case int64:
		return int(raw)
	case float64:
		return int(raw)
	default:
		return 0
	}
}

func stringMapFromConfigMap(config map[string]interface{}, key string) map[string]string {
	if config == nil {
		return nil
	}
	switch raw := config[key].(type) {
	case map[string]string:
		out := map[string]string{}
		for k, v := range raw {
			out[k] = v
		}
		return out
	case map[string]interface{}:
		out := map[string]string{}
		for k, v := range raw {
			if s, ok := v.(string); ok {
				out[k] = s
			}
		}
		return out
	default:
		return nil
	}
}

// resolveMCPServerSpecFromRow converts a stored MCP server row into the shared
// transport-neutral spec.
func resolveMCPServerSpecFromRow(row database.MCPIntegrationConfig) MCPServerSpec {
	config := map[string]interface{}{}
	_ = json.Unmarshal(row.Config, &config)
	spec := MCPServerSpec{
		Name:      row.Name,
		Enabled:   row.Enabled,
		Transport: normalizeMCPServerTransport(stringFromConfigMap(config, "transport")),
		URL:       stringFromConfigMap(config, "url"),
		Command:   stringFromConfigMap(config, "command"),
		Args:      stringSliceFromConfigMap(config, "args"),
		Env:       stringMapFromConfigMap(config, "env"),
		Headers:   stringMapFromConfigMap(config, "headers"),
		AuthMode:  authModeFromConfig(config),
		AuthData:  extractAuthData(row.AuthData),
		Config:    config,
		TimeoutMS: intFromConfigMap(config, "request_timeout_seconds") * 1000,
	}
	if spec.Transport == "" {
		switch {
		case spec.URL != "":
			spec.Transport = MCPServerTransportHTTPStreamable
		case spec.Command != "":
			spec.Transport = MCPServerTransportStdio
		}
	}
	return spec
}

// mcpServerConfigFromSpec is the inverse of resolveMCPServerSpecFromRow: it
// renders a spec into the stored config shape accepted by validateServerRequest.
func mcpServerConfigFromSpec(spec MCPServerSpec) map[string]interface{} {
	config := map[string]interface{}{}
	for k, v := range spec.Config {
		config[k] = v
	}
	transport := normalizeMCPServerTransport(spec.Transport)
	if transport == "" {
		switch {
		case strings.TrimSpace(spec.URL) != "":
			transport = MCPServerTransportHTTPStreamable
		case strings.TrimSpace(spec.Command) != "":
			transport = MCPServerTransportStdio
		}
	}
	if transport != "" {
		config["transport"] = transport
	}
	if url := strings.TrimSpace(spec.URL); url != "" {
		config["url"] = url
	}
	if command := strings.TrimSpace(spec.Command); command != "" {
		config["command"] = command
	}
	if len(spec.Args) > 0 {
		config["args"] = append([]string(nil), spec.Args...)
	}
	if len(spec.Env) > 0 {
		config["env"] = spec.Env
	}
	if len(spec.Headers) > 0 {
		config["headers"] = spec.Headers
	}
	if spec.TimeoutMS > 0 {
		config["request_timeout_seconds"] = (spec.TimeoutMS + 999) / 1000
	}
	if mode := strings.TrimSpace(spec.AuthMode); mode != "" {
		auth, _ := config["auth"].(map[string]interface{})
		if auth == nil {
			auth = map[string]interface{}{}
		}
		auth["mode"] = mode
		config["auth"] = auth
	}
	return config
}

// ResolveMCPServerSpecs returns the owner's registered MCP servers as shared
// specs. When names is non-empty only those servers are returned; unknown names
// are ignored. Disabled servers are omitted.
func ResolveMCPServerSpecs(db *gorm.DB, ownerUserID uint, names []string) ([]MCPServerSpec, error) {
	if db == nil {
		return nil, fmt.Errorf("db is required")
	}
	query := db.Where("owner_user_id = ?", ownerUserID)
	if len(names) > 0 {
		normalized := make([]string, 0, len(names))
		for _, name := range names {
			if v := normalizeServerName(name); v != "" {
				normalized = append(normalized, v)
			}
		}
		if len(normalized) == 0 {
			return []MCPServerSpec{}, nil
		}
		query = query.Where("name IN ?", normalized)
	}

	rows := []database.MCPIntegrationConfig{}
	if err := query.Order("name asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]MCPServerSpec, 0, len(rows))
	for _, row := range rows {
		if !row.Enabled {
			continue
		}
		out = append(out, resolveMCPServerSpecFromRow(row))
	}
	return out, nil
}

// ResolveMCPServerSpec returns one owner-scoped MCP server as a shared spec.
func ResolveMCPServerSpec(db *gorm.DB, ownerUserID uint, name string) (MCPServerSpec, error) {
	if db == nil {
		return MCPServerSpec{}, fmt.Errorf("db is required")
	}
	row, err := loadOwnedServer(db, ownerUserID, normalizeServerName(name))
	if err != nil {
		return MCPServerSpec{}, err
	}
	return resolveMCPServerSpecFromRow(row), nil
}

// RegisterMCPServer idempotently creates or updates an owner-scoped MCP server
// from a shared spec. It reuses the HTTP API's validation and known-host OAuth
// inference, so an integration can register a server programmatically with the
// same guarantees as the UI, and refreshes the owner's bot tool snapshots.
func RegisterMCPServer(db *gorm.DB, ownerUserID uint, spec MCPServerSpec) (MCPServerSpec, error) {
	if db == nil {
		return MCPServerSpec{}, fmt.Errorf("db is required")
	}
	if ownerUserID == 0 {
		return MCPServerSpec{}, fmt.Errorf("owner user id is required")
	}
	enabled := spec.Enabled
	req, err := validateServerRequest(serverUpsertRequest{
		Name:    spec.Name,
		Config:  mcpServerConfigFromSpec(spec),
		Enabled: &enabled,
	})
	if err != nil {
		return MCPServerSpec{}, err
	}
	configJSON, err := json.Marshal(req.Config)
	if err != nil {
		return MCPServerSpec{}, fmt.Errorf("failed to encode config: %w", err)
	}
	authData := json.RawMessage("{}")
	if len(spec.AuthData) > 0 {
		encoded, encErr := json.Marshal(spec.AuthData)
		if encErr != nil {
			return MCPServerSpec{}, fmt.Errorf("failed to encode auth_data: %w", encErr)
		}
		authData = encoded
	}

	existing, err := loadOwnedServer(db, ownerUserID, req.Name)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return MCPServerSpec{}, err
		}
		// The Enabled column carries a gorm `default:true` tag, so a zero-value
		// false would be omitted on insert. Create with the default and, when a
		// disabled server is requested, persist the explicit false afterwards.
		row := database.MCPIntegrationConfig{
			OwnerUserId: ownerUserID,
			Name:        req.Name,
			Config:      configJSON,
			AuthData:    authData,
			AuthSession: json.RawMessage("{}"),
			Enabled:     true,
		}
		if err := db.Create(&row).Error; err != nil {
			return MCPServerSpec{}, err
		}
		if !enabled {
			if err := db.Model(&row).Update("enabled", false).Error; err != nil {
				return MCPServerSpec{}, err
			}
			row.Enabled = false
		}
		refreshOwnerBotMCPToolSnapshotsBestEffort(db, ownerUserID)
		return resolveMCPServerSpecFromRow(row), nil
	}

	updates := map[string]interface{}{
		"config":  configJSON,
		"enabled": enabled,
	}
	if len(spec.AuthData) > 0 {
		updates["auth_data"] = authData
	}
	if err := db.Model(&existing).Updates(updates).Error; err != nil {
		return MCPServerSpec{}, err
	}
	if err := db.Where("id = ?", existing.ID).First(&existing).Error; err != nil {
		return MCPServerSpec{}, err
	}
	refreshOwnerBotMCPToolSnapshotsBestEffort(db, ownerUserID)
	return resolveMCPServerSpecFromRow(existing), nil
}

// UnregisterMCPServer deletes an owner-scoped MCP server by name and refreshes
// the owner's bot tool snapshots.
func UnregisterMCPServer(db *gorm.DB, ownerUserID uint, name string) error {
	if db == nil {
		return fmt.Errorf("db is required")
	}
	normalized := normalizeServerName(name)
	if normalized == "" {
		return fmt.Errorf("name is required")
	}
	if err := db.Where("owner_user_id = ? AND name = ?", ownerUserID, normalized).Delete(&database.MCPIntegrationConfig{}).Error; err != nil {
		return err
	}
	refreshOwnerBotMCPToolSnapshotsBestEffort(db, ownerUserID)
	return nil
}

func refreshOwnerBotMCPToolSnapshotsBestEffort(db *gorm.DB, ownerUserID uint) {
	if err := refreshOwnerBotMCPToolSnapshots(db, ownerUserID); err != nil {
		log.Printf("mcp registry snapshot refresh failed for user %d: %v", ownerUserID, err)
	}
}

// BuiltinMCPServerSpec returns a preconfigured, sandbox-local MCP server spec
// for a well-known preset. The Playwright preset runs the browser next to the
// agent (stdio), so consumers running inside the coding sandbox (eg the opencode
// integration) can enable browser automation with a single toggle and no
// separately registered server.
func BuiltinMCPServerSpec(preset string) (MCPServerSpec, bool) {
	switch normalizeServerName(preset) {
	case MCPPresetPlaywright:
		return MCPServerSpec{
			Name:      MCPPresetPlaywright,
			Enabled:   true,
			Transport: MCPServerTransportStdio,
			// npx downloads the package and launches the browser on first use,
			// which can exceed the consumer's default tool-discovery timeout.
			TimeoutMS: 60000,
			Command:   "npx",
			Args: []string{
				"-y",
				"@playwright/mcp@latest",
				"--headless",
				"--no-sandbox",
				"--isolated",
				"--executable-path",
				"/usr/bin/chromium-browser",
			},
		}, true
	default:
		return MCPServerSpec{}, false
	}
}
