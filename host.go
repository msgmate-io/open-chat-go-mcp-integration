package mcpintegration

import (
	"errors"
	"net/http"
	"strings"

	"github.com/msgmate-io/go-integration-interface/integrationinterface"
	"gorm.io/gorm"
)

var errHostNotInstalled = errors.New("mcp integration: host is not installed")

// hostConfig returns the resolved runtime configuration through the stable Host
// surface. It replaces the previous direct backend/runtimecfg import. When no
// Host is installed (standalone tooling, or an integration test process that
// never wires the core adapter) it returns an empty config instead of panicking.
func hostConfig() integrationinterface.Config {
	if host := integrationinterface.GetHost(); host != nil {
		return host.Config()
	}
	return emptyConfig{}
}

type emptyConfig struct{}

func (emptyConfig) All() map[string]integrationinterface.Value {
	return map[string]integrationinterface.Value{}
}

func (emptyConfig) Get(string) (integrationinterface.Value, bool) {
	return integrationinterface.Value{}, false
}

// publicBaseURL mirrors backend/runtimecfg.PublicBaseURL without importing the
// core package: it reads the resolved PUBLIC_BASE_URL runtime value.
func publicBaseURL() string {
	value, _ := hostConfig().Get("PUBLIC_BASE_URL")
	return strings.TrimSpace(value.Value)
}

// dbAndUser resolves the request-scoped database handle and authenticated user
// through the Host surface. It is the counterpart of backend/server/util
// GetDBAndUser.
func dbAndUser(r *http.Request) (*gorm.DB, integrationinterface.User, error) {
	host := integrationinterface.GetHost()
	if host == nil {
		return nil, nil, errHostNotInstalled
	}
	storage, user, err := host.HTTP().DBAndUser(r)
	if err != nil {
		return nil, nil, err
	}
	db, _ := storage.Handle().(*gorm.DB)
	return db, user, nil
}
