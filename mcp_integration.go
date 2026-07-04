package mcpintegration

import (
	"backend/api/msgmate"
	"backend/database"
	"backend/server/util"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/msgmate-io/go-integration-interface/integrationinterface"
	"gorm.io/gorm"
)

type serverUpsertRequest struct {
	Name    string                 `json:"name"`
	Config  map[string]interface{} `json:"config"`
	Enabled *bool                  `json:"enabled,omitempty"`
}

type authStartResponse struct {
	Success      bool     `json:"success"`
	Mode         string   `json:"mode"`
	AuthorizeURL string   `json:"authorize_url,omitempty"`
	State        string   `json:"state,omitempty"`
	Message      string   `json:"message,omitempty"`
	Required     []string `json:"required,omitempty"`
}

type authCompleteRequest struct {
	BearerToken string `json:"bearer_token,omitempty"`
	Code        string `json:"code,omitempty"`
	State       string `json:"state,omitempty"`
}

type storedOAuthSession struct {
	Mode           string `json:"mode"`
	State          string `json:"state"`
	CodeVerifier   string `json:"code_verifier,omitempty"`
	ClientID       string `json:"client_id,omitempty"`
	ClientSecret   string `json:"client_secret,omitempty"`
	TokenURL       string `json:"token_url,omitempty"`
	Scope          string `json:"scope,omitempty"`
	Resource       string `json:"resource,omitempty"`
	RedirectURI    string `json:"redirect_uri,omitempty"`
	CreatedAtUnix  int64  `json:"created_at_unix"`
	AuthorizeURL   string `json:"authorize_url,omitempty"`
	ExpectedServer string `json:"expected_server,omitempty"`
}

type serverAuthStatus struct {
	Mode      string `json:"mode"`
	Connected bool   `json:"connected"`
	Pending   bool   `json:"pending"`
}

type oauthDiscoveredConfig struct {
	AuthorizeURL         string
	TokenURL             string
	RegistrationEndpoint string
	Scope                string
	Resource             string
}

type serverResponseRow struct {
	Name          string                 `json:"name"`
	Config        map[string]interface{} `json:"config"`
	Enabled       bool                   `json:"enabled"`
	HasAuthData   bool                   `json:"has_auth_data"`
	AuthMode      string                 `json:"auth_mode"`
	AuthConnected bool                   `json:"auth_connected"`
	AuthPending   bool                   `json:"auth_pending"`
	CreatedAtUnix int64                  `json:"created_at_unix"`
	UpdatedAtUnix int64                  `json:"updated_at_unix"`
}

var serverNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,80}$`)

var mcpAPIRoutes = []string{
	"GET /api/v1/integrations/mcp/servers",
	"POST /api/v1/integrations/mcp/servers",
	"PUT /api/v1/integrations/mcp/servers/{server_name}",
	"DELETE /api/v1/integrations/mcp/servers/{server_name}",
	"POST /api/v1/integrations/mcp/servers/{server_name}/discover",
	"GET /api/v1/integrations/mcp/auth/callback",
	"GET /api/v1/integrations/mcp/servers/{server_name}/auth/status",
	"POST /api/v1/integrations/mcp/servers/{server_name}/auth/start",
	"POST /api/v1/integrations/mcp/servers/{server_name}/auth/complete",
	"POST /api/v1/integrations/mcp/servers/{server_name}/auth/clear",
}

var mcpAPIRouteDocs = []integrationinterface.APIRouteDoc{
	{
		Route:        mcpAPIRoutes[0],
		Summary:      "List MCP servers",
		Description:  "List owner-scoped MCP servers registered via the MCP integration.",
		RequiredAuth: []string{"SessionAuth"},
	},
	{
		Route:        mcpAPIRoutes[1],
		Summary:      "Create MCP server",
		Description:  "Create an owner-scoped MCP server registration.",
		RequiredAuth: []string{"SessionAuth"},
	},
	{
		Route:        mcpAPIRoutes[2],
		Summary:      "Update MCP server",
		Description:  "Update an owner-scoped MCP server registration.",
		RequiredAuth: []string{"SessionAuth"},
		Parameters: []integrationinterface.APIRouteParameter{
			{Name: "server_name", In: "path", Type: "string", Required: true, Description: "Server Name"},
		},
	},
	{
		Route:        mcpAPIRoutes[3],
		Summary:      "Delete MCP server",
		Description:  "Delete an owner-scoped MCP server registration.",
		RequiredAuth: []string{"SessionAuth"},
		Parameters: []integrationinterface.APIRouteParameter{
			{Name: "server_name", In: "path", Type: "string", Required: true, Description: "Server Name"},
		},
	},
	{
		Route:        mcpAPIRoutes[4],
		Summary:      "Discover MCP server tools",
		Description:  "Calls tools/list on a registered MCP server.",
		RequiredAuth: []string{"SessionAuth"},
		Parameters: []integrationinterface.APIRouteParameter{
			{Name: "server_name", In: "path", Type: "string", Required: true, Description: "Server Name"},
		},
	},
	{
		Route:        mcpAPIRoutes[5],
		Summary:      "MCP OAuth callback endpoint",
		Description:  "Completes OAuth flow using code+state query params and redirects to integration UI.",
		RequiredAuth: []string{"SessionAuth"},
		Parameters: []integrationinterface.APIRouteParameter{
			{Name: "code", In: "query", Type: "string", Required: true, Description: "OAuth authorization code"},
			{Name: "state", In: "query", Type: "string", Required: true, Description: "OAuth state value"},
			{Name: "error", In: "query", Type: "string", Required: false, Description: "OAuth provider error code"},
		},
	},
	{
		Route:        mcpAPIRoutes[6],
		Summary:      "Get MCP server auth status",
		Description:  "Returns current authentication status for a registered MCP server.",
		RequiredAuth: []string{"SessionAuth"},
		Parameters: []integrationinterface.APIRouteParameter{
			{Name: "server_name", In: "path", Type: "string", Required: true, Description: "Server Name"},
		},
	},
	{
		Route:        mcpAPIRoutes[7],
		Summary:      "Start MCP server auth flow",
		Description:  "Starts authentication flow for a registered MCP server.",
		RequiredAuth: []string{"SessionAuth"},
		Parameters: []integrationinterface.APIRouteParameter{
			{Name: "server_name", In: "path", Type: "string", Required: true, Description: "Server Name"},
		},
	},
	{
		Route:        mcpAPIRoutes[8],
		Summary:      "Complete MCP server auth flow",
		Description:  "Completes authentication flow and stores auth_data server-side.",
		RequiredAuth: []string{"SessionAuth"},
		Parameters: []integrationinterface.APIRouteParameter{
			{Name: "server_name", In: "path", Type: "string", Required: true, Description: "Server Name"},
		},
	},
	{
		Route:        mcpAPIRoutes[9],
		Summary:      "Clear MCP server auth data",
		Description:  "Clears stored auth_data and pending auth session for a server.",
		RequiredAuth: []string{"SessionAuth"},
		Parameters: []integrationinterface.APIRouteParameter{
			{Name: "server_name", In: "path", Type: "string", Required: true, Description: "Server Name"},
		},
	},
}

//go:embed frontend_assets
var mcpFrontendAssets embed.FS

//go:embed README.md
var mcpReadmeMarkdown string

var mcpFrontendPages = []integrationinterface.FrontendPage{
	{
		Route:       "/integrations/mcp/servers",
		Public:      false,
		Description: "MCP server list and management links.",
		AssetPath:   "servers/index.html",
	},
	{
		Route:       "/integrations/mcp/servers/add",
		Public:      false,
		Description: "MCP add-server form page.",
		AssetPath:   "servers/add/index.html",
	},
}

func init() {
	integrationinterface.MustRegister(integrationinterface.Definition{
		Name:           "mcp",
		ReadmeMarkdown: strings.TrimSpace(mcpReadmeMarkdown),
		APIRoutes:      append([]string(nil), mcpAPIRoutes...),
		APIRouteDocs:   append([]integrationinterface.APIRouteDoc(nil), mcpAPIRouteDocs...),
		FrontendPages:  append([]integrationinterface.FrontendPage(nil), mcpFrontendPages...),
		FrontendAssets: mustSubFS(mcpFrontendAssets, "frontend_assets"),
		ModelProviders: []func() []interface{}{
			func() []interface{} {
				return []interface{}{&database.MCPIntegrationConfig{}}
			},
		},
		RouteRegistrar: registerRoutes,
		Functions: map[string]integrationinterface.Function{
			"discover_tools": func(_ context.Context, payload map[string]interface{}) (interface{}, error) {
				config, _ := payload["config"].(map[string]interface{})
				auth, _ := payload["auth_data"].(map[string]interface{})
				tools, err := msgmate.DiscoverMCPTools(config, auth)
				if err != nil {
					return nil, err
				}
				return tools, nil
			},
		},
	})
}

func mustSubFS(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

func registerRoutes(v1Private *http.ServeMux, _ *http.ServeMux) {
	v1Private.HandleFunc(v1PrivatePattern(mcpAPIRoutes[0]), listServers)
	v1Private.HandleFunc(v1PrivatePattern(mcpAPIRoutes[1]), createServer)
	v1Private.HandleFunc(v1PrivatePattern(mcpAPIRoutes[2]), updateServer)
	v1Private.HandleFunc(v1PrivatePattern(mcpAPIRoutes[3]), deleteServer)
	v1Private.HandleFunc(v1PrivatePattern(mcpAPIRoutes[4]), discoverServer)
	v1Private.HandleFunc(v1PrivatePattern(mcpAPIRoutes[5]), authCallback)
	v1Private.HandleFunc(v1PrivatePattern(mcpAPIRoutes[6]), authStatus)
	v1Private.HandleFunc(v1PrivatePattern(mcpAPIRoutes[7]), authStart)
	v1Private.HandleFunc(v1PrivatePattern(mcpAPIRoutes[8]), authComplete)
	v1Private.HandleFunc(v1PrivatePattern(mcpAPIRoutes[9]), authClear)
}

func v1PrivatePattern(fullRoute string) string {
	const v1Prefix = "/api/v1"
	idx := strings.Index(fullRoute, " ")
	if idx < 0 || idx+1 >= len(fullRoute) {
		return fullRoute
	}
	method := strings.TrimSpace(fullRoute[:idx])
	path := strings.TrimSpace(fullRoute[idx+1:])
	if strings.HasPrefix(path, v1Prefix) {
		path = strings.TrimPrefix(path, v1Prefix)
		if path == "" {
			path = "/"
		}
	}
	return method + " " + path
}

func normalizeServerName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func authConfig(config map[string]interface{}) map[string]interface{} {
	if config == nil {
		return map[string]interface{}{}
	}
	authRaw, _ := config["auth"].(map[string]interface{})
	if authRaw == nil {
		return map[string]interface{}{}
	}
	return authRaw
}

func explicitAuthMode(config map[string]interface{}) string {
	auth := authConfig(config)
	mode, _ := auth["mode"].(string)
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "", "none":
		return "none"
	case "bearer", "bearer_token", "token":
		return "bearer_token"
	case "oauth", "oauth2", "oauth2_code":
		return "oauth2"
	default:
		return mode
	}
}

func inferredOAuthConfig(config map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range authConfig(config) {
		out[k] = v
	}
	urlRaw, _ := config["url"].(string)
	parsedURL, err := url.Parse(strings.TrimSpace(urlRaw))
	if err != nil {
		return out
	}
	host := strings.ToLower(strings.TrimSpace(parsedURL.Hostname()))
	if host == "mcp.figma.com" {
		if v, _ := out["authorize_url"].(string); strings.TrimSpace(v) == "" {
			out["authorize_url"] = "https://www.figma.com/oauth"
		}
		if v, _ := out["token_url"].(string); strings.TrimSpace(v) == "" {
			out["token_url"] = "https://api.figma.com/v1/oauth/token"
		}
		if v, _ := out["redirect_uri"].(string); strings.TrimSpace(v) == "" {
			out["redirect_uri"] = "http://localhost:3000/callback"
		}
		if _, ok := out["scopes"]; !ok {
			out["scopes"] = []string{"file_content:read"}
		}
		if _, ok := out["use_pkce"]; !ok {
			out["use_pkce"] = true
		}
	}
	return out
}

func parseWWWAuthenticateParams(headerValue string) map[string]string {
	params := map[string]string{}
	headerValue = strings.TrimSpace(headerValue)
	if headerValue == "" {
		return params
	}
	if idx := strings.Index(headerValue, " "); idx >= 0 {
		headerValue = strings.TrimSpace(headerValue[idx+1:])
	}
	matches := regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_-]*)="([^"]*)"`).FindAllStringSubmatch(headerValue, -1)
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		params[strings.ToLower(strings.TrimSpace(m[1]))] = strings.TrimSpace(m[2])
	}
	return params
}

func fetchJSONMap(targetURL string) (map[string]interface{}, error) {
	req, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func discoverOAuthConfigFromMCPServer(config map[string]interface{}, fallback map[string]interface{}) (oauthDiscoveredConfig, error) {
	serverURLRaw, _ := config["url"].(string)
	serverURL := strings.TrimSpace(serverURLRaw)
	if serverURL == "" {
		return oauthDiscoveredConfig{}, fmt.Errorf("config.url is required")
	}
	reqBody := map[string]interface{}{"jsonrpc": "2.0", "id": "auth-discovery", "method": "tools/list", "params": map[string]interface{}{}}
	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequest(http.MethodPost, serverURL, strings.NewReader(string(body)))
	if err != nil {
		return oauthDiscoveredConfig{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return oauthDiscoveredConfig{}, err
	}
	defer resp.Body.Close()

	challenge := parseWWWAuthenticateParams(resp.Header.Get("WWW-Authenticate"))
	out := oauthDiscoveredConfig{}
	if scopeRaw, ok := challenge["scope"]; ok {
		out.Scope = strings.TrimSpace(scopeRaw)
	}

	if authURI, ok := challenge["authorization_uri"]; ok {
		out.AuthorizeURL = strings.TrimSpace(authURI)
		if strings.Contains(out.AuthorizeURL, "/.well-known/oauth-authorization-server") {
			authMetadata, err := fetchJSONMap(out.AuthorizeURL)
			if err == nil {
				if authorizeEndpoint, ok := authMetadata["authorization_endpoint"].(string); ok && strings.TrimSpace(authorizeEndpoint) != "" {
					out.AuthorizeURL = strings.TrimSpace(authorizeEndpoint)
				}
				if tokenEndpoint, ok := authMetadata["token_endpoint"].(string); ok && strings.TrimSpace(tokenEndpoint) != "" {
					out.TokenURL = strings.TrimSpace(tokenEndpoint)
				}
				if regEndpoint, ok := authMetadata["registration_endpoint"].(string); ok && strings.TrimSpace(regEndpoint) != "" {
					out.RegistrationEndpoint = strings.TrimSpace(regEndpoint)
				}
				if scopesSupported, ok := authMetadata["scopes_supported"]; ok && out.Scope == "" {
					switch v := scopesSupported.(type) {
					case []interface{}:
						parts := make([]string, 0, len(v))
						for _, item := range v {
							if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
								parts = append(parts, strings.TrimSpace(s))
							}
						}
						if len(parts) > 0 {
							out.Scope = strings.Join(parts, " ")
						}
					}
				}
			}
		}
	}

	if resourceMetadataURL, ok := challenge["resource_metadata"]; ok && strings.TrimSpace(resourceMetadataURL) != "" {
		resourceMetadata, err := fetchJSONMap(strings.TrimSpace(resourceMetadataURL))
		if err == nil {
			if resourceRaw, ok := resourceMetadata["resource"].(string); ok {
				out.Resource = strings.TrimSpace(resourceRaw)
			}
			if scopesRaw, ok := resourceMetadata["scopes_supported"]; ok && out.Scope == "" {
				switch v := scopesRaw.(type) {
				case []interface{}:
					parts := make([]string, 0, len(v))
					for _, item := range v {
						if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
							parts = append(parts, strings.TrimSpace(s))
						}
					}
					if len(parts) > 0 {
						out.Scope = strings.Join(parts, " ")
					}
				}
			}
			if serversRaw, ok := resourceMetadata["authorization_servers"].([]interface{}); ok && len(serversRaw) > 0 {
				if serverURL, ok := serversRaw[0].(string); ok && strings.TrimSpace(serverURL) != "" {
					authMetadataURL := strings.TrimSuffix(strings.TrimSpace(serverURL), "/") + "/.well-known/oauth-authorization-server"
					authMetadata, err := fetchJSONMap(authMetadataURL)
					if err == nil {
						if authorizeEndpoint, ok := authMetadata["authorization_endpoint"].(string); ok && strings.TrimSpace(authorizeEndpoint) != "" {
							out.AuthorizeURL = strings.TrimSpace(authorizeEndpoint)
						}
						if tokenEndpoint, ok := authMetadata["token_endpoint"].(string); ok && strings.TrimSpace(tokenEndpoint) != "" {
							out.TokenURL = strings.TrimSpace(tokenEndpoint)
						}
						if regEndpoint, ok := authMetadata["registration_endpoint"].(string); ok && strings.TrimSpace(regEndpoint) != "" {
							out.RegistrationEndpoint = strings.TrimSpace(regEndpoint)
						}
						if scopesSupported, ok := authMetadata["scopes_supported"]; ok && out.Scope == "" {
							switch v := scopesSupported.(type) {
							case []interface{}:
								parts := make([]string, 0, len(v))
								for _, item := range v {
									if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
										parts = append(parts, strings.TrimSpace(s))
									}
								}
								if len(parts) > 0 {
									out.Scope = strings.Join(parts, " ")
								}
							}
						}
					}
				}
			}
		}
	}

	if out.AuthorizeURL == "" {
		if v, _ := fallback["authorize_url"].(string); strings.TrimSpace(v) != "" {
			out.AuthorizeURL = strings.TrimSpace(v)
		}
	}
	if out.TokenURL == "" {
		if v, _ := fallback["token_url"].(string); strings.TrimSpace(v) != "" {
			out.TokenURL = strings.TrimSpace(v)
		}
	}
	if out.Scope == "" {
		out.Scope = authScopes(fallback)
	}
	if out.Resource == "" {
		out.Resource = serverURL
	}

	if out.AuthorizeURL == "" || out.TokenURL == "" {
		return out, fmt.Errorf("could not discover OAuth endpoints from MCP challenge")
	}
	return out, nil
}

func dynamicClientRegistration(registrationEndpoint string, redirectURI string) (string, string, error) {
	if strings.TrimSpace(registrationEndpoint) == "" {
		return "", "", fmt.Errorf("registration endpoint is empty")
	}
	payload := map[string]interface{}{
		"client_name":                "Codex",
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "client_secret_post",
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, registrationEndpoint, strings.NewReader(string(body)))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Codex")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", "", fmt.Errorf("registration failed: %s: %s", resp.Status, strings.TrimSpace(string(responseBody)))
	}
	result := map[string]interface{}{}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return "", "", err
	}
	clientID, _ := result["client_id"].(string)
	clientSecret, _ := result["client_secret"].(string)
	if strings.TrimSpace(clientID) == "" {
		return "", "", fmt.Errorf("registration response missing client_id")
	}
	return strings.TrimSpace(clientID), strings.TrimSpace(clientSecret), nil
}

func authModeFromConfig(config map[string]interface{}) string {
	mode := explicitAuthMode(config)
	if mode != "none" {
		return mode
	}
	urlRaw, _ := config["url"].(string)
	parsedURL, err := url.Parse(strings.TrimSpace(urlRaw))
	if err != nil {
		return mode
	}
	if strings.EqualFold(parsedURL.Hostname(), "mcp.figma.com") {
		return "oauth2"
	}
	return mode
}

func probeMCPAuthConnection(config map[string]interface{}, authData map[string]interface{}) bool {
	if len(authData) == 0 {
		return false
	}
	probeConfig := map[string]interface{}{}
	for k, v := range config {
		probeConfig[k] = v
	}
	timeoutSeconds := 5
	if raw, ok := probeConfig["request_timeout_seconds"]; ok {
		switch v := raw.(type) {
		case float64:
			if v >= 1 && v < 5 {
				timeoutSeconds = int(v)
			}
		case int:
			if v >= 1 && v < 5 {
				timeoutSeconds = v
			}
		}
	}
	probeConfig["request_timeout_seconds"] = timeoutSeconds
	_, err := msgmate.DiscoverMCPTools(probeConfig, authData)
	return err == nil
}

func authStatusFor(config map[string]interface{}, authData map[string]interface{}, session storedOAuthSession) serverAuthStatus {
	mode := authModeFromConfig(config)
	connected := false
	pending := strings.TrimSpace(session.State) != ""
	if mode == "none" {
		connected = true
		pending = false
	} else {
		connected = probeMCPAuthConnection(config, authData)
	}
	return serverAuthStatus{Mode: mode, Connected: connected, Pending: pending}
}

func decodeStoredOAuthSession(raw []byte) storedOAuthSession {
	session := storedOAuthSession{}
	if len(raw) == 0 {
		return session
	}
	_ = json.Unmarshal(raw, &session)
	return session
}

func encodeStoredOAuthSession(session storedOAuthSession) []byte {
	if strings.TrimSpace(session.State) == "" {
		return []byte("{}")
	}
	encoded, _ := json.Marshal(session)
	if len(encoded) == 0 {
		return []byte("{}")
	}
	return encoded
}

func randomURLSafeToken(size int) (string, error) {
	if size < 16 {
		size = 16
	}
	raw := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func pkceChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func authScopes(authCfg map[string]interface{}) string {
	if scopesRaw, ok := authCfg["scopes"]; ok {
		switch v := scopesRaw.(type) {
		case string:
			return strings.TrimSpace(v)
		case []interface{}:
			parts := make([]string, 0, len(v))
			for _, item := range v {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					parts = append(parts, strings.TrimSpace(s))
				}
			}
			return strings.Join(parts, " ")
		}
	}
	return ""
}

func boolFromMap(m map[string]interface{}, key string, def bool) bool {
	v, ok := m[key]
	if !ok {
		return def
	}
	b, ok := v.(bool)
	if !ok {
		return def
	}
	return b
}

func extractAuthData(raw json.RawMessage) map[string]interface{} {
	out := map[string]interface{}{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

func loadOwnedServer(DB *gorm.DB, ownerUserID uint, name string) (database.MCPIntegrationConfig, error) {
	row := database.MCPIntegrationConfig{}
	err := DB.Where("owner_user_id = ? AND name = ?", ownerUserID, name).First(&row).Error
	return row, err
}

func findOwnedServerByOAuthState(DB *gorm.DB, ownerUserID uint, state string) (database.MCPIntegrationConfig, storedOAuthSession, error) {
	rows := []database.MCPIntegrationConfig{}
	if err := DB.Where("owner_user_id = ?", ownerUserID).Find(&rows).Error; err != nil {
		return database.MCPIntegrationConfig{}, storedOAuthSession{}, err
	}
	for _, row := range rows {
		session := decodeStoredOAuthSession(row.AuthSession)
		if strings.TrimSpace(session.State) == strings.TrimSpace(state) {
			return row, session, nil
		}
	}
	return database.MCPIntegrationConfig{}, storedOAuthSession{}, gorm.ErrRecordNotFound
}

func validateServerRequest(req serverUpsertRequest) (serverUpsertRequest, error) {
	req.Name = normalizeServerName(req.Name)
	if req.Name == "" {
		return req, fmt.Errorf("name is required")
	}
	if !serverNamePattern.MatchString(req.Name) {
		return req, fmt.Errorf("name must match ^[a-z0-9][a-z0-9_-]{1,80}$")
	}
	if req.Config == nil {
		return req, fmt.Errorf("config is required")
	}
	mode := explicitAuthMode(req.Config)
	switch mode {
	case "none", "bearer_token", "oauth2":
	default:
		return req, fmt.Errorf("config.auth.mode must be one of: none, bearer_token, oauth2")
	}
	if mode == "oauth2" {
		authCfg := inferredOAuthConfig(req.Config)
		authorizeURL, _ := authCfg["authorize_url"].(string)
		tokenURL, _ := authCfg["token_url"].(string)
		redirectURI, _ := authCfg["redirect_uri"].(string)
		if strings.TrimSpace(authorizeURL) == "" {
			return req, fmt.Errorf("config.auth.authorize_url is required for oauth2")
		}
		if strings.TrimSpace(tokenURL) == "" {
			return req, fmt.Errorf("config.auth.token_url is required for oauth2")
		}
		if strings.TrimSpace(redirectURI) == "" {
			return req, fmt.Errorf("config.auth.redirect_uri is required for oauth2")
		}
	}
	return req, nil
}

func decodeServerRow(row database.MCPIntegrationConfig) serverResponseRow {
	config := map[string]interface{}{}
	_ = json.Unmarshal(row.Config, &config)
	authData := extractAuthData(row.AuthData)
	hasAuthData := len(authData) > 0
	authSession := decodeStoredOAuthSession(row.AuthSession)
	authStatus := authStatusFor(config, authData, authSession)
	return serverResponseRow{
		Name:          row.Name,
		Config:        config,
		Enabled:       row.Enabled,
		HasAuthData:   hasAuthData,
		AuthMode:      authStatus.Mode,
		AuthConnected: authStatus.Connected,
		AuthPending:   authStatus.Pending,
		CreatedAtUnix: row.CreatedAt.Unix(),
		UpdatedAtUnix: row.UpdatedAt.Unix(),
	}
}

// List servers
// @Summary      List MCP servers
// @Description  List owner-scoped MCP servers registered via the MCP integration.
// @Tags         integrations
// @Produce      json
// @Security     SessionAuth
// @Success      200 {object} map[string]interface{}
// @Router       /api/v1/integrations/mcp/servers [get]
func listServers(w http.ResponseWriter, r *http.Request) {
	DB, user, err := util.GetDBAndUser(r)
	if err != nil {
		http.Error(w, "Unable to get database or user", http.StatusBadRequest)
		return
	}
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	rows := []database.MCPIntegrationConfig{}
	if err := DB.Where("owner_user_id = ?", user.ID).Order("name asc").Find(&rows).Error; err != nil {
		http.Error(w, "Failed to list MCP servers", http.StatusInternalServerError)
		return
	}
	items := make([]serverResponseRow, 0, len(rows))
	for _, row := range rows {
		items = append(items, decodeServerRow(row))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"rows": items})
}

// Create server
// @Summary      Create MCP server
// @Description  Create an owner-scoped MCP server registration.
// @Tags         integrations
// @Accept       json
// @Produce      json
// @Security     SessionAuth
// @Success      200 {object} map[string]interface{}
// @Router       /api/v1/integrations/mcp/servers [post]
func createServer(w http.ResponseWriter, r *http.Request) {
	DB, user, err := util.GetDBAndUser(r)
	if err != nil {
		http.Error(w, "Unable to get database or user", http.StatusBadRequest)
		return
	}
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var req serverUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	req, err = validateServerRequest(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	configJSON, _ := json.Marshal(req.Config)
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	row := database.MCPIntegrationConfig{
		OwnerUserId: user.ID,
		Name:        req.Name,
		Config:      configJSON,
		AuthData:    json.RawMessage("{}"),
		AuthSession: json.RawMessage("{}"),
		Enabled:     enabled,
	}
	if err := DB.Create(&row).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "unique") {
			http.Error(w, "server already exists", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to create MCP server", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "row": decodeServerRow(row)})
}

// Update server
// @Summary      Update MCP server
// @Description  Update an owner-scoped MCP server registration.
// @Tags         integrations
// @Accept       json
// @Produce      json
// @Security     SessionAuth
// @Param        server_name path string true "Server Name"
// @Success      200 {object} map[string]interface{}
// @Router       /api/v1/integrations/mcp/servers/{server_name} [put]
func updateServer(w http.ResponseWriter, r *http.Request) {
	DB, user, err := util.GetDBAndUser(r)
	if err != nil {
		http.Error(w, "Unable to get database or user", http.StatusBadRequest)
		return
	}
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	name := normalizeServerName(r.PathValue("server_name"))
	if name == "" {
		http.Error(w, "server_name is required", http.StatusBadRequest)
		return
	}
	var req serverUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = name
	}
	req, err = validateServerRequest(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var existing database.MCPIntegrationConfig
	if err := DB.Where("owner_user_id = ? AND name = ?", user.ID, name).First(&existing).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "server not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to load MCP server", http.StatusInternalServerError)
		return
	}
	configJSON, _ := json.Marshal(req.Config)
	updates := map[string]interface{}{
		"name":         req.Name,
		"config":       configJSON,
		"auth_data":    json.RawMessage("{}"),
		"auth_session": json.RawMessage("{}"),
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if err := DB.Model(&existing).Updates(updates).Error; err != nil {
		http.Error(w, "Failed to update MCP server", http.StatusInternalServerError)
		return
	}
	if err := DB.Where("id = ?", existing.ID).First(&existing).Error; err != nil {
		http.Error(w, "Failed to load MCP server", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "row": decodeServerRow(existing)})
}

// Delete server
// @Summary      Delete MCP server
// @Description  Delete an owner-scoped MCP server registration.
// @Tags         integrations
// @Produce      json
// @Security     SessionAuth
// @Param        server_name path string true "Server Name"
// @Success      200 {object} map[string]interface{}
// @Router       /api/v1/integrations/mcp/servers/{server_name} [delete]
func deleteServer(w http.ResponseWriter, r *http.Request) {
	DB, user, err := util.GetDBAndUser(r)
	if err != nil {
		http.Error(w, "Unable to get database or user", http.StatusBadRequest)
		return
	}
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	name := normalizeServerName(r.PathValue("server_name"))
	if name == "" {
		http.Error(w, "server_name is required", http.StatusBadRequest)
		return
	}
	if err := DB.Where("owner_user_id = ? AND name = ?", user.ID, name).Delete(&database.MCPIntegrationConfig{}).Error; err != nil {
		http.Error(w, "Failed to delete MCP server", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// Discover server
// @Summary      Discover MCP server tools
// @Description  Calls tools/list on a registered MCP server.
// @Tags         integrations
// @Produce      json
// @Security     SessionAuth
// @Param        server_name path string true "Server Name"
// @Success      200 {object} map[string]interface{}
// @Router       /api/v1/integrations/mcp/servers/{server_name}/discover [post]
func discoverServer(w http.ResponseWriter, r *http.Request) {
	DB, user, err := util.GetDBAndUser(r)
	if err != nil {
		http.Error(w, "Unable to get database or user", http.StatusBadRequest)
		return
	}
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	name := normalizeServerName(r.PathValue("server_name"))
	if name == "" {
		http.Error(w, "server_name is required", http.StatusBadRequest)
		return
	}
	var row database.MCPIntegrationConfig
	if err := DB.Where("owner_user_id = ? AND name = ?", user.ID, name).First(&row).Error; err != nil {
		http.Error(w, "server not found", http.StatusNotFound)
		return
	}
	config := map[string]interface{}{}
	_ = json.Unmarshal(row.Config, &config)
	auth := extractAuthData(row.AuthData)
	tools, err := msgmate.DiscoverMCPTools(config, auth)
	if err != nil {
		http.Error(w, fmt.Sprintf("Discovery failed: %v", err), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "count": len(tools), "tools": tools})
}

// Auth status
// @Summary      Get MCP server auth status
// @Description  Returns current authentication status for a registered MCP server.
// @Tags         integrations
// @Produce      json
// @Security     SessionAuth
// @Param        server_name path string true "Server Name"
// @Success      200 {object} map[string]interface{}
// @Router       /api/v1/integrations/mcp/servers/{server_name}/auth/status [get]
func authStatus(w http.ResponseWriter, r *http.Request) {
	DB, user, err := util.GetDBAndUser(r)
	if err != nil {
		http.Error(w, "Unable to get database or user", http.StatusBadRequest)
		return
	}
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	name := normalizeServerName(r.PathValue("server_name"))
	if name == "" {
		http.Error(w, "server_name is required", http.StatusBadRequest)
		return
	}
	row, err := loadOwnedServer(DB, user.ID, name)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "server not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to load MCP server", http.StatusInternalServerError)
		return
	}

	config := map[string]interface{}{}
	_ = json.Unmarshal(row.Config, &config)
	auth := extractAuthData(row.AuthData)
	session := decodeStoredOAuthSession(row.AuthSession)
	status := authStatusFor(config, auth, session)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"server_name":   row.Name,
		"auth_mode":     status.Mode,
		"connected":     status.Connected,
		"pending":       status.Pending,
		"has_auth_data": len(auth) > 0,
	})
}

// Auth callback
// @Summary      MCP OAuth callback endpoint
// @Description  Completes OAuth flow using code+state query params and redirects to integration UI.
// @Tags         integrations
// @Produce      json
// @Security     SessionAuth
// @Success      302 {string} string "redirect"
// @Router       /api/v1/integrations/mcp/auth/callback [get]
func authCallback(w http.ResponseWriter, r *http.Request) {
	DB, user, err := util.GetDBAndUser(r)
	if err != nil || user == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	providerErr := strings.TrimSpace(r.URL.Query().Get("error"))
	if providerErr != "" {
		http.Redirect(w, r, "/integrations/mcp/servers?auth_error="+url.QueryEscape(providerErr), http.StatusFound)
		return
	}
	if code == "" || state == "" {
		http.Redirect(w, r, "/integrations/mcp/servers?auth_error=missing_code_or_state", http.StatusFound)
		return
	}

	row, session, err := findOwnedServerByOAuthState(DB, user.ID, state)
	if err != nil {
		http.Redirect(w, r, "/integrations/mcp/servers?auth_error=state_not_found", http.StatusFound)
		return
	}

	config := map[string]interface{}{}
	_ = json.Unmarshal(row.Config, &config)
	authCfg := inferredOAuthConfig(config)
	tokenData, err := oauthTokenExchange(authCfg, authCompleteRequest{Code: code, State: state}, session)
	if err != nil {
		errMsg := url.QueryEscape(err.Error())
		http.Redirect(w, r, "/integrations/mcp/servers?auth_error="+errMsg, http.StatusFound)
		return
	}
	authJSON, _ := json.Marshal(tokenData)
	if len(authJSON) == 0 {
		authJSON = []byte("{}")
	}
	if err := DB.Model(&row).Updates(map[string]interface{}{
		"auth_data":    json.RawMessage(authJSON),
		"auth_session": json.RawMessage("{}"),
	}).Error; err != nil {
		errMsg := url.QueryEscape("Failed to persist auth data")
		http.Redirect(w, r, "/integrations/mcp/servers?auth_error="+errMsg, http.StatusFound)
		return
	}
	http.Redirect(w, r, "/integrations/mcp/servers?auth_success=1&server="+url.QueryEscape(row.Name), http.StatusFound)
}

// Auth start
// @Summary      Start MCP server auth flow
// @Description  Starts authentication flow for a registered MCP server.
// @Tags         integrations
// @Produce      json
// @Security     SessionAuth
// @Param        server_name path string true "Server Name"
// @Success      200 {object} map[string]interface{}
// @Router       /api/v1/integrations/mcp/servers/{server_name}/auth/start [post]
func authStart(w http.ResponseWriter, r *http.Request) {
	DB, user, err := util.GetDBAndUser(r)
	if err != nil {
		http.Error(w, "Unable to get database or user", http.StatusBadRequest)
		return
	}
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	name := normalizeServerName(r.PathValue("server_name"))
	if name == "" {
		http.Error(w, "server_name is required", http.StatusBadRequest)
		return
	}
	row, err := loadOwnedServer(DB, user.ID, name)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "server not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to load MCP server", http.StatusInternalServerError)
		return
	}

	config := map[string]interface{}{}
	_ = json.Unmarshal(row.Config, &config)
	authCfg := inferredOAuthConfig(config)
	mode := authModeFromConfig(config)

	resp := authStartResponse{Success: true, Mode: mode}
	switch mode {
	case "none":
		resp.Message = "No authentication required for this server."
	case "bearer_token":
		resp.Required = []string{"bearer_token"}
		resp.Message = "Provide bearer_token to complete authentication."
	case "oauth2":
		discovered, err := discoverOAuthConfigFromMCPServer(config, authCfg)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to discover oauth endpoints: %v", err), http.StatusBadGateway)
			return
		}
		authorizeURLRaw := discovered.AuthorizeURL
		clientID, _ := authCfg["client_id"].(string)
		clientSecret, _ := authCfg["client_secret"].(string)
		redirectURI, _ := authCfg["redirect_uri"].(string)
		if strings.TrimSpace(authorizeURLRaw) == "" || strings.TrimSpace(redirectURI) == "" {
			http.Error(w, "config.auth.authorize_url and config.auth.redirect_uri are required", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(clientID) == "" {
			if strings.TrimSpace(discovered.RegistrationEndpoint) == "" {
				http.Error(w, "config.auth.client_id is required for oauth2 (registration endpoint unavailable)", http.StatusBadRequest)
				return
			}
			registeredID, registeredSecret, regErr := dynamicClientRegistration(discovered.RegistrationEndpoint, strings.TrimSpace(redirectURI))
			if regErr != nil {
				http.Error(w, fmt.Sprintf("oauth client registration failed: %v", regErr), http.StatusBadGateway)
				return
			}
			clientID = registeredID
			if strings.TrimSpace(clientSecret) == "" {
				clientSecret = registeredSecret
			}
		}
		state, err := randomURLSafeToken(24)
		if err != nil {
			http.Error(w, "Failed generating OAuth state", http.StatusInternalServerError)
			return
		}
		verifier, err := randomURLSafeToken(48)
		if err != nil {
			http.Error(w, "Failed generating OAuth verifier", http.StatusInternalServerError)
			return
		}
		authorizeURL, err := url.Parse(strings.TrimSpace(authorizeURLRaw))
		if err != nil {
			http.Error(w, "invalid config.auth.authorize_url", http.StatusBadRequest)
			return
		}
		q := authorizeURL.Query()
		q.Set("response_type", "code")
		q.Set("client_id", strings.TrimSpace(clientID))
		q.Set("redirect_uri", strings.TrimSpace(redirectURI))
		q.Set("state", state)
		scopes := strings.TrimSpace(discovered.Scope)
		if scopes == "" {
			scopes = authScopes(authCfg)
		}
		if scopes != "" {
			q.Set("scope", scopes)
		}
		if strings.TrimSpace(discovered.Resource) != "" {
			q.Set("resource", strings.TrimSpace(discovered.Resource))
		}
		if boolFromMap(authCfg, "use_pkce", true) {
			q.Set("code_challenge", pkceChallenge(verifier))
			q.Set("code_challenge_method", "S256")
		}
		if extra, ok := authCfg["authorize_params"].(map[string]interface{}); ok {
			for k, v := range extra {
				if strings.TrimSpace(k) == "" {
					continue
				}
				q.Set(k, strings.TrimSpace(fmt.Sprintf("%v", v)))
			}
		}
		authorizeURL.RawQuery = q.Encode()

		session := storedOAuthSession{
			Mode:           mode,
			State:          state,
			CodeVerifier:   verifier,
			ClientID:       strings.TrimSpace(clientID),
			ClientSecret:   strings.TrimSpace(clientSecret),
			TokenURL:       strings.TrimSpace(discovered.TokenURL),
			Scope:          scopes,
			Resource:       strings.TrimSpace(discovered.Resource),
			RedirectURI:    strings.TrimSpace(redirectURI),
			CreatedAtUnix:  time.Now().Unix(),
			AuthorizeURL:   authorizeURL.String(),
			ExpectedServer: name,
		}
		if err := DB.Model(&row).Update("auth_session", encodeStoredOAuthSession(session)).Error; err != nil {
			http.Error(w, "Failed to save OAuth session", http.StatusInternalServerError)
			return
		}
		resp.State = state
		resp.AuthorizeURL = authorizeURL.String()
		resp.Message = "Open authorize_url to complete OAuth. Redirect callback will finalize auth automatically."
	default:
		http.Error(w, "unsupported config.auth.mode", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func oauthTokenExchange(authCfg map[string]interface{}, req authCompleteRequest, session storedOAuthSession) (map[string]interface{}, error) {
	tokenURLRaw := strings.TrimSpace(session.TokenURL)
	if tokenURLRaw == "" {
		v, _ := authCfg["token_url"].(string)
		tokenURLRaw = strings.TrimSpace(v)
	}
	clientID := strings.TrimSpace(session.ClientID)
	if clientID == "" {
		v, _ := authCfg["client_id"].(string)
		clientID = strings.TrimSpace(v)
	}
	clientSecret := strings.TrimSpace(session.ClientSecret)
	if clientSecret == "" {
		v, _ := authCfg["client_secret"].(string)
		clientSecret = strings.TrimSpace(v)
	}
	redirectURI := strings.TrimSpace(session.RedirectURI)
	if redirectURI == "" {
		v, _ := authCfg["redirect_uri"].(string)
		redirectURI = strings.TrimSpace(v)
	}
	if strings.TrimSpace(tokenURLRaw) == "" {
		return nil, fmt.Errorf("config.auth.token_url is required")
	}
	if strings.TrimSpace(clientID) == "" {
		return nil, fmt.Errorf("config.auth.client_id is required")
	}
	tokenURL, err := url.Parse(strings.TrimSpace(tokenURLRaw))
	if err != nil {
		return nil, fmt.Errorf("invalid config.auth.token_url")
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", strings.TrimSpace(req.Code))
	form.Set("client_id", strings.TrimSpace(clientID))
	if strings.TrimSpace(clientSecret) != "" {
		form.Set("client_secret", strings.TrimSpace(clientSecret))
	}
	if strings.TrimSpace(redirectURI) != "" {
		form.Set("redirect_uri", strings.TrimSpace(redirectURI))
	}
	if strings.TrimSpace(session.CodeVerifier) != "" {
		form.Set("code_verifier", session.CodeVerifier)
	}
	if strings.TrimSpace(session.Resource) != "" {
		form.Set("resource", strings.TrimSpace(session.Resource))
	}

	httpReq, err := http.NewRequest(http.MethodPost, tokenURL.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("token request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	result := map[string]interface{}{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("invalid token response JSON: %w", err)
	}
	result["obtained_at_unix"] = time.Now().Unix()
	return result, nil
}

// Auth complete
// @Summary      Complete MCP server auth flow
// @Description  Completes authentication flow and stores auth_data server-side.
// @Tags         integrations
// @Accept       json
// @Produce      json
// @Security     SessionAuth
// @Param        server_name path string true "Server Name"
// @Success      200 {object} map[string]interface{}
// @Router       /api/v1/integrations/mcp/servers/{server_name}/auth/complete [post]
func authComplete(w http.ResponseWriter, r *http.Request) {
	DB, user, err := util.GetDBAndUser(r)
	if err != nil {
		http.Error(w, "Unable to get database or user", http.StatusBadRequest)
		return
	}
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	name := normalizeServerName(r.PathValue("server_name"))
	if name == "" {
		http.Error(w, "server_name is required", http.StatusBadRequest)
		return
	}
	row, err := loadOwnedServer(DB, user.ID, name)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "server not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to load MCP server", http.StatusInternalServerError)
		return
	}

	requestBody := authCompleteRequest{}
	if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	config := map[string]interface{}{}
	_ = json.Unmarshal(row.Config, &config)
	mode := authModeFromConfig(config)
	authCfg := inferredOAuthConfig(config)
	storedAuth := map[string]interface{}{}

	switch mode {
	case "none":
		storedAuth = map[string]interface{}{}
	case "bearer_token":
		token := strings.TrimSpace(requestBody.BearerToken)
		if token == "" {
			http.Error(w, "bearer_token is required", http.StatusBadRequest)
			return
		}
		storedAuth = map[string]interface{}{
			"bearer_token": token,
		}
	case "oauth2":
		session := decodeStoredOAuthSession(row.AuthSession)
		if strings.TrimSpace(session.State) == "" {
			http.Error(w, "auth/start must be called before auth/complete", http.StatusBadRequest)
			return
		}
		if time.Unix(session.CreatedAtUnix, 0).Add(15 * time.Minute).Before(time.Now()) {
			http.Error(w, "oauth session expired, restart with auth/start", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(requestBody.Code) == "" {
			http.Error(w, "code is required", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(requestBody.State) == "" || strings.TrimSpace(requestBody.State) != strings.TrimSpace(session.State) {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		tokenData, err := oauthTokenExchange(authCfg, requestBody, session)
		if err != nil {
			http.Error(w, fmt.Sprintf("oauth token exchange failed: %v", err), http.StatusBadGateway)
			return
		}
		storedAuth = tokenData
	default:
		http.Error(w, "unsupported config.auth.mode", http.StatusBadRequest)
		return
	}

	authJSON, _ := json.Marshal(storedAuth)
	if len(authJSON) == 0 {
		authJSON = []byte("{}")
	}
	updates := map[string]interface{}{
		"auth_data":    json.RawMessage(authJSON),
		"auth_session": json.RawMessage("{}"),
	}
	if err := DB.Model(&row).Updates(updates).Error; err != nil {
		http.Error(w, "Failed to store auth data", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "auth_mode": mode})
}

// Auth clear
// @Summary      Clear MCP server auth data
// @Description  Clears stored auth_data and pending auth session for a server.
// @Tags         integrations
// @Produce      json
// @Security     SessionAuth
// @Param        server_name path string true "Server Name"
// @Success      200 {object} map[string]interface{}
// @Router       /api/v1/integrations/mcp/servers/{server_name}/auth/clear [post]
func authClear(w http.ResponseWriter, r *http.Request) {
	DB, user, err := util.GetDBAndUser(r)
	if err != nil {
		http.Error(w, "Unable to get database or user", http.StatusBadRequest)
		return
	}
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	name := normalizeServerName(r.PathValue("server_name"))
	if name == "" {
		http.Error(w, "server_name is required", http.StatusBadRequest)
		return
	}
	row, err := loadOwnedServer(DB, user.ID, name)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "server not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to load MCP server", http.StatusInternalServerError)
		return
	}
	if err := DB.Model(&row).Updates(map[string]interface{}{
		"auth_data":    json.RawMessage("{}"),
		"auth_session": json.RawMessage("{}"),
	}).Error; err != nil {
		http.Error(w, "Failed to clear auth data", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}
