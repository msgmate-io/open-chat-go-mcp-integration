package mcpintegration

import (
	"backend/api/msgmate"
	"backend/database"
	"backend/server/util"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/msgmate-io/go-integration-interface/integrationinterface"
	"gorm.io/gorm"
)

type serverUpsertRequest struct {
	Name     string                 `json:"name"`
	Config   map[string]interface{} `json:"config"`
	AuthData map[string]interface{} `json:"auth_data,omitempty"`
	Enabled  *bool                  `json:"enabled,omitempty"`
}

type serverResponseRow struct {
	Name          string                 `json:"name"`
	Config        map[string]interface{} `json:"config"`
	Enabled       bool                   `json:"enabled"`
	HasAuthData   bool                   `json:"has_auth_data"`
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
}

//go:embed frontend_assets
var mcpFrontendAssets embed.FS

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
		APIRoutes:      append([]string(nil), mcpAPIRoutes...),
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
	if _, err := msgmate.DiscoverMCPTools(req.Config, req.AuthData); err != nil {
		return req, fmt.Errorf("unable to connect to MCP server: %w", err)
	}
	return req, nil
}

func decodeServerRow(row database.MCPIntegrationConfig) serverResponseRow {
	config := map[string]interface{}{}
	_ = json.Unmarshal(row.Config, &config)
	hasAuthData := len(strings.TrimSpace(string(row.AuthData))) > 0 && strings.TrimSpace(string(row.AuthData)) != "{}"
	return serverResponseRow{
		Name:          row.Name,
		Config:        config,
		Enabled:       row.Enabled,
		HasAuthData:   hasAuthData,
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
	authJSON, _ := json.Marshal(req.AuthData)
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	row := database.MCPIntegrationConfig{
		OwnerUserId: user.ID,
		Name:        req.Name,
		Config:      configJSON,
		AuthData:    authJSON,
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
	authJSON, _ := json.Marshal(req.AuthData)
	updates := map[string]interface{}{
		"name":      req.Name,
		"config":    configJSON,
		"auth_data": authJSON,
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
	auth := map[string]interface{}{}
	_ = json.Unmarshal(row.Config, &config)
	_ = json.Unmarshal(row.AuthData, &auth)
	tools, err := msgmate.DiscoverMCPTools(config, auth)
	if err != nil {
		http.Error(w, fmt.Sprintf("Discovery failed: %v", err), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "count": len(tools), "tools": tools})
}
