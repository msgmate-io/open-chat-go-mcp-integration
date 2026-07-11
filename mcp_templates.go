package mcpintegration

import (
	"backend/server/util"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"sort"
	"strings"
)

type mcpTemplateManifest struct {
	ID                string                 `json:"id"`
	DisplayName       string                 `json:"display_name"`
	Description       string                 `json:"description"`
	DefaultServerName string                 `json:"default_server_name"`
	Tags              []string               `json:"tags,omitempty"`
	IsRecommended     bool                   `json:"is_recommended,omitempty"`
	Config            map[string]interface{} `json:"config"`
}

type mcpTemplateResponseRow struct {
	ID                string                 `json:"id"`
	DisplayName       string                 `json:"display_name"`
	Description       string                 `json:"description"`
	DefaultServerName string                 `json:"default_server_name"`
	Tags              []string               `json:"tags"`
	IsRecommended     bool                   `json:"is_recommended"`
	LogoDataURL       string                 `json:"logo_data_url,omitempty"`
	Config            map[string]interface{} `json:"config"`
	ReadmeMarkdown    string                 `json:"readme_markdown"`
}

//go:embed mcp_templates/**
var mcpTemplateLibraryFS embed.FS

func normalizeTag(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func logoMimeTypeByFileName(fileName string) string {
	name := strings.ToLower(strings.TrimSpace(fileName))
	switch {
	case strings.HasSuffix(name, ".svg.webp"), strings.HasSuffix(name, ".webp"):
		return "image/webp"
	case strings.HasSuffix(name, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(name, ".png"):
		return "image/png"
	case strings.HasSuffix(name, ".jpg"), strings.HasSuffix(name, ".jpeg"):
		return "image/jpeg"
	default:
		return ""
	}
}

func loadMCPTemplateLogoDataURL(templateDir string) (string, error) {
	logoCandidates := []string{"logo.svg.webp", "logo.webp", "logo.svg", "logo.png", "logo.jpg", "logo.jpeg"}
	for _, logoFile := range logoCandidates {
		assetPath := "mcp_templates/" + templateDir + "/" + logoFile
		logoRaw, err := mcpTemplateLibraryFS.ReadFile(assetPath)
		if err != nil {
			continue
		}
		mimeType := logoMimeTypeByFileName(logoFile)
		if mimeType == "" {
			continue
		}
		encoded := base64.StdEncoding.EncodeToString(logoRaw)
		if strings.TrimSpace(encoded) == "" {
			continue
		}
		return "data:" + mimeType + ";base64," + encoded, nil
	}
	return "", nil
}

func loadMCPTemplateLibrary() ([]mcpTemplateResponseRow, error) {
	entries, err := fs.ReadDir(mcpTemplateLibraryFS, "mcp_templates")
	if err != nil {
		return nil, err
	}
	out := make([]mcpTemplateResponseRow, 0, len(entries))
	seenTemplateIDs := map[string]struct{}{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		templateDir := strings.TrimSpace(entry.Name())
		if templateDir == "" {
			continue
		}

		templatePath := "mcp_templates/" + templateDir + "/template.json"
		readmePath := "mcp_templates/" + templateDir + "/README.md"
		templateRaw, err := mcpTemplateLibraryFS.ReadFile(templatePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read %q: %w", templatePath, err)
		}
		readmeRaw, err := mcpTemplateLibraryFS.ReadFile(readmePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read %q: %w", readmePath, err)
		}

		var manifest mcpTemplateManifest
		if err := json.Unmarshal(templateRaw, &manifest); err != nil {
			return nil, fmt.Errorf("failed to parse %q: %w", templatePath, err)
		}

		manifest.ID = normalizeServerName(manifest.ID)
		manifest.DisplayName = strings.TrimSpace(manifest.DisplayName)
		manifest.Description = strings.TrimSpace(manifest.Description)
		manifest.DefaultServerName = normalizeServerName(manifest.DefaultServerName)
		if manifest.ID == "" {
			return nil, fmt.Errorf("template %q missing id", templateDir)
		}
		if !serverNamePattern.MatchString(manifest.ID) {
			return nil, fmt.Errorf("template %q has invalid id %q", templateDir, manifest.ID)
		}
		if manifest.DisplayName == "" {
			return nil, fmt.Errorf("template %q missing display_name", manifest.ID)
		}
		if manifest.DefaultServerName == "" {
			manifest.DefaultServerName = manifest.ID
		}
		if !serverNamePattern.MatchString(manifest.DefaultServerName) {
			return nil, fmt.Errorf("template %q has invalid default_server_name %q", manifest.ID, manifest.DefaultServerName)
		}
		if manifest.Config == nil || len(manifest.Config) == 0 {
			return nil, fmt.Errorf("template %q missing config object", manifest.ID)
		}
		if _, exists := seenTemplateIDs[manifest.ID]; exists {
			return nil, fmt.Errorf("duplicate MCP template id %q", manifest.ID)
		}
		seenTemplateIDs[manifest.ID] = struct{}{}

		logoDataURL, err := loadMCPTemplateLogoDataURL(templateDir)
		if err != nil {
			return nil, err
		}

		tags := make([]string, 0, len(manifest.Tags))
		seenTags := map[string]struct{}{}
		for _, tag := range manifest.Tags {
			normalizedTag := normalizeTag(tag)
			if normalizedTag == "" {
				continue
			}
			if _, exists := seenTags[normalizedTag]; exists {
				continue
			}
			tags = append(tags, normalizedTag)
			seenTags[normalizedTag] = struct{}{}
		}
		sort.Strings(tags)

		out = append(out, mcpTemplateResponseRow{
			ID:                manifest.ID,
			DisplayName:       manifest.DisplayName,
			Description:       manifest.Description,
			DefaultServerName: manifest.DefaultServerName,
			Tags:              tags,
			IsRecommended:     manifest.IsRecommended,
			LogoDataURL:       logoDataURL,
			Config:            manifest.Config,
			ReadmeMarkdown:    strings.TrimSpace(string(readmeRaw)),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].IsRecommended != out[j].IsRecommended {
			return out[i].IsRecommended
		}
		if out[i].DisplayName != out[j].DisplayName {
			return out[i].DisplayName < out[j].DisplayName
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// List templates
// @Summary      List MCP server templates
// @Description  List built-in MCP configuration templates and setup documentation.
// @Tags         integrations
// @Produce      json
// @Security     SessionAuth
// @Success      200 {object} map[string]interface{}
// @Router       /api/v1/integrations/mcp/templates [get]
func listTemplates(w http.ResponseWriter, r *http.Request) {
	_, user, err := util.GetDBAndUser(r)
	if err != nil {
		http.Error(w, "Unable to get database or user", http.StatusBadRequest)
		return
	}
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	rows, err := loadMCPTemplateLibrary()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load MCP templates: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"rows": rows})
}
