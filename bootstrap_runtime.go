package mcpintegration

import (
	"backend/runtimecfg"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"gorm.io/gorm"
)

const (
	bootstrapServersEnvKey       = "OCI_MCP_BOOTSTRAP_SERVERS"
	bootstrapDefaultOwnersEnvKey = "OCI_MCP_BOOTSTRAP_DEFAULT_OWNERS"
)

// ApplyRuntimeConfigBootstrap applies the MCP bootstrap sources declared
// through the integration's own runtime env vars (OCI_MCP_BOOTSTRAP_*).
// Spec values may be inline JSON objects/arrays or paths to JSON files; the
// default owner references are a comma-separated string. The env keys can be
// set directly or through open-chat.json (integrations.mcp.bootstrap_*).
func ApplyRuntimeConfigBootstrap(db *gorm.DB, fallbackOwner string) (BootstrapResult, error) {
	if db == nil {
		return BootstrapResult{}, fmt.Errorf("db is required")
	}
	all := runtimecfg.GetAll()

	defaultOwners := splitMCPOwnerRefs(all[bootstrapDefaultOwnersEnvKey].Value)
	serverSpecs, err := decodeMCPSpecValue[BootstrapServerSpec](all[bootstrapServersEnvKey].Value)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("failed to load mcp bootstrap servers spec: %w", err)
	}
	if len(defaultOwners) == 0 && len(serverSpecs) == 0 {
		return BootstrapResult{}, nil
	}

	return ApplyBootstrap(db, BootstrapSpec{
		FallbackOwner: fallbackOwner,
		DefaultOwners: defaultOwners,
		Servers:       serverSpecs,
	})
}

func splitMCPOwnerRefs(raw string) []string {
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// decodeMCPSpecValue decodes one-or-many JSON specs from an inline JSON string
// or a filesystem path.
func decodeMCPSpecValue[T any](value string) ([]T, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return []T{}, nil
	}
	raw, source, err := resolveMCPSpecBytes(trimmed)
	if err != nil {
		return nil, err
	}
	return decodeMCPSpecJSON[T](raw, source)
}

func resolveMCPSpecBytes(spec string) ([]byte, string, error) {
	trimmed := strings.TrimSpace(spec)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return []byte(trimmed), "inline", nil
	}
	raw, err := os.ReadFile(trimmed)
	if err != nil {
		return nil, "", fmt.Errorf("failed reading bootstrap spec file %q: %w", trimmed, err)
	}
	return raw, trimmed, nil
}

func decodeMCPSpecJSON[T any](raw []byte, source string) ([]T, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return []T{}, nil
	}

	if trimmed[0] == '[' {
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.DisallowUnknownFields()
		var out []T
		if err := decoder.Decode(&out); err != nil {
			return nil, fmt.Errorf("invalid JSON array in %s: %w", source, err)
		}
		var trailing interface{}
		if err := decoder.Decode(&trailing); err != io.EOF {
			if err == nil {
				return nil, fmt.Errorf("invalid JSON array in %s: unexpected trailing JSON", source)
			}
			return nil, fmt.Errorf("invalid JSON array in %s: %w", source, err)
		}
		return out, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var single T
	if err := decoder.Decode(&single); err != nil {
		return nil, fmt.Errorf("invalid JSON object in %s: %w", source, err)
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("invalid JSON object in %s: unexpected trailing JSON", source)
		}
		return nil, fmt.Errorf("invalid JSON object in %s: %w", source, err)
	}
	return []T{single}, nil
}