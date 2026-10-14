package mcpintegration

import (
	"os"
	"testing"

	"backend/integrationhost"
)

// TestMain installs the core Host adapter so the integration's config and
// request helpers (which now go through integrationinterface.RequireHost) work
// in unit tests. Tests still drive runtimecfg.SetAll directly; the adapter reads
// the resolved values dynamically.
func TestMain(m *testing.M) {
	integrationhost.Install()
	os.Exit(m.Run())
}
