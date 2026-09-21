package mcpintegration

import (
	"backend/database"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"gorm.io/gorm"
)

// BootstrapServerSpec declaratively seeds an owner-scoped MCP server at server
// startup. All fields except Name are optional: a built-in template id can
// supply the base config and Config is deep-merged on top of it.
type BootstrapServerSpec struct {
	Owner     string                 `json:"owner,omitempty"`
	Owners    []string               `json:"owners,omitempty"`
	Name      string                 `json:"name"`
	Template  string                 `json:"template,omitempty"`
	Config    map[string]interface{} `json:"config,omitempty"`
	AuthData  map[string]interface{} `json:"auth_data,omitempty"`
	Enabled   *bool                  `json:"enabled,omitempty"`
	Overwrite bool                   `json:"overwrite,omitempty"`
}

// BootstrapSpec mirrors the git/ssh integrations' bootstrap contract:
// declarative, idempotent upserts applied from open-chat.json
// (bootstrap.mcp) or integration-owned runtime env keys.
type BootstrapSpec struct {
	FallbackOwner string
	DefaultOwners []string
	Servers       []BootstrapServerSpec
}

type BootstrapResult struct {
	ServersCreated int
	ServersUpdated int
	ServersSkipped int
}

func normalizeBootstrapOwnerRefs(perEntryOwner string, perEntryOwners []string, defaultOwners []string, fallbackOwner string) []string {
	merged := []string{}
	if strings.TrimSpace(perEntryOwner) != "" {
		merged = append(merged, perEntryOwner)
	}
	merged = append(merged, perEntryOwners...)
	if len(merged) == 0 {
		merged = append(merged, defaultOwners...)
	}
	if len(merged) == 0 && strings.TrimSpace(fallbackOwner) != "" {
		merged = append(merged, fallbackOwner)
	}

	seen := map[string]struct{}{}
	out := []string{}
	for _, owner := range merged {
		trimmed := strings.TrimSpace(owner)
		if trimmed == "" {
			continue
		}
		norm := strings.ToLower(trimmed)
		if _, exists := seen[norm]; exists {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func resolveBootstrapOwnerByReference(db *gorm.DB, reference string) (*database.User, error) {
	ref := strings.TrimSpace(reference)
	if ref == "" {
		return nil, fmt.Errorf("owner reference is required")
	}

	var user database.User
	if err := db.Where("username = ?", ref).First(&user).Error; err == nil {
		return &user, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	if err := db.Where("email = ? OR name = ?", ref, ref).First(&user).Error; err == nil {
		return &user, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	return nil, fmt.Errorf("owner %q not found", ref)
}

// templateConfigByID resolves a built-in template's base config by id.
func templateConfigByID(id string) (map[string]interface{}, error) {
	target := normalizeServerName(id)
	if target == "" {
		return nil, fmt.Errorf("template id is required")
	}
	rows, err := loadMCPTemplateLibrary()
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if normalizeServerName(row.ID) == target {
			return deepMergeMaps(map[string]interface{}{}, row.Config), nil
		}
	}
	return nil, fmt.Errorf("unknown mcp template %q", id)
}

// deepMergeMaps recursively merges override into base. Nested maps are merged
// key-by-key; every other value (including arrays) is replaced by override.
func deepMergeMaps(base map[string]interface{}, override map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		overrideMap, overrideIsMap := v.(map[string]interface{})
		baseMap, baseIsMap := out[k].(map[string]interface{})
		if overrideIsMap && baseIsMap {
			out[k] = deepMergeMaps(baseMap, overrideMap)
			continue
		}
		out[k] = v
	}
	return out
}

// buildBootstrapServerConfig resolves the effective config for a spec by
// starting from the built-in template (when configured) and deep-merging the
// spec overrides on top. Known-host OAuth defaults are filled in, then the
// config is validated with the same rules used by the API.
func buildBootstrapServerConfig(spec BootstrapServerSpec) (serverUpsertRequest, error) {
	effective := map[string]interface{}{}
	if strings.TrimSpace(spec.Template) != "" {
		templateCfg, err := templateConfigByID(spec.Template)
		if err != nil {
			return serverUpsertRequest{}, err
		}
		effective = templateCfg
	}
	effective = deepMergeMaps(effective, spec.Config)
	if len(effective) == 0 {
		return serverUpsertRequest{}, fmt.Errorf("config is required (set config and/or template)")
	}

	if authCfg := inferredOAuthConfig(effective); len(authCfg) > 0 {
		effective["auth"] = authCfg
	}

	return validateServerRequest(serverUpsertRequest{
		Name:    spec.Name,
		Config:  effective,
		Enabled: spec.Enabled,
	})
}

// ApplyBootstrap applies the declarative MCP server bootstrap spec
// idempotently. Rows are matched by (owner, name). Existing rows are skipped
// unless Overwrite is set, which preserves UI-managed config and OAuth tokens
// across restarts.
func ApplyBootstrap(db *gorm.DB, spec BootstrapSpec) (BootstrapResult, error) {
	result := BootstrapResult{}
	if db == nil {
		return result, fmt.Errorf("db is required")
	}

	ownerCache := map[string]*database.User{}
	resolveOwner := func(reference string) (*database.User, error) {
		norm := strings.ToLower(strings.TrimSpace(reference))
		if norm == "" {
			return nil, fmt.Errorf("owner reference is required")
		}
		if user, ok := ownerCache[norm]; ok && user != nil {
			return user, nil
		}
		user, err := resolveBootstrapOwnerByReference(db, reference)
		if err != nil {
			return nil, err
		}
		ownerCache[norm] = user
		return user, nil
	}

	ownersRefreshed := map[uint]struct{}{}
	for idx, serverSpec := range spec.Servers {
		req, err := buildBootstrapServerConfig(serverSpec)
		if err != nil {
			return result, fmt.Errorf("mcp server bootstrap[%d]: %w", idx, err)
		}
		configJSON, err := json.Marshal(req.Config)
		if err != nil {
			return result, fmt.Errorf("mcp server bootstrap[%d]: failed to encode config: %w", idx, err)
		}

		owners := normalizeBootstrapOwnerRefs(serverSpec.Owner, serverSpec.Owners, spec.DefaultOwners, spec.FallbackOwner)
		if len(owners) == 0 {
			return result, fmt.Errorf("mcp server bootstrap[%d] has no owner and no fallback owner configured", idx)
		}
		for _, ownerRef := range owners {
			owner, err := resolveOwner(ownerRef)
			if err != nil {
				return result, fmt.Errorf("mcp server bootstrap[%d]: %w", idx, err)
			}
			existing, err := loadOwnedServer(db, owner.ID, req.Name)
			isCreate := false
			if err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return result, err
				}
				isCreate = true
			}

			enabled := true
			if serverSpec.Enabled != nil {
				enabled = *serverSpec.Enabled
			}

			if isCreate {
				authData := json.RawMessage("{}")
				if len(serverSpec.AuthData) > 0 {
					encoded, encErr := json.Marshal(serverSpec.AuthData)
					if encErr != nil {
						return result, fmt.Errorf("mcp server bootstrap[%d]: failed to encode auth_data: %w", idx, encErr)
					}
					authData = encoded
				}
				row := database.MCPIntegrationConfig{
					OwnerUserId: owner.ID,
					Name:        req.Name,
					Config:      configJSON,
					AuthData:    authData,
					AuthSession: json.RawMessage("{}"),
					Enabled:     enabled,
				}
				if err := db.Create(&row).Error; err != nil {
					return result, err
				}
				result.ServersCreated++
			} else if !serverSpec.Overwrite {
				result.ServersSkipped++
			} else {
				updates := map[string]interface{}{
					"config":  configJSON,
					"enabled": enabled,
				}
				if len(serverSpec.AuthData) > 0 {
					encoded, encErr := json.Marshal(serverSpec.AuthData)
					if encErr != nil {
						return result, fmt.Errorf("mcp server bootstrap[%d]: failed to encode auth_data: %w", idx, encErr)
					}
					updates["auth_data"] = json.RawMessage(encoded)
				}
				if err := db.Model(&existing).Updates(updates).Error; err != nil {
					return result, err
				}
				result.ServersUpdated++
			}

			ownersRefreshed[owner.ID] = struct{}{}
		}
	}

	for ownerID := range ownersRefreshed {
		if err := refreshOwnerBotMCPToolSnapshots(db, ownerID); err != nil {
			log.Printf("mcp bootstrap snapshot refresh failed for user %d: %v", ownerID, err)
		}
	}

	return result, nil
}