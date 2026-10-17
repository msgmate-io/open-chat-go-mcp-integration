package mcpintegration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mcpStubServer stands up a minimal MCP streamable-http server: it answers the
// initialize handshake and returns `callResult` for a tools/call. Each test gets
// its own server (and therefore its own URL, so the process-wide session cache
// never crosses tests).
func mcpStubServer(t *testing.T, callResult map[string]interface{}) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": "open-chat", "result": callResult})
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": "open-chat", "result": map[string]interface{}{}})
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// TestCallMCPToolReturnsContentText guards the tools/call path: the MCP content
// text blocks are concatenated into the returned string.
func TestCallMCPToolReturnsContentText(t *testing.T) {
	server := mcpStubServer(t, map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": "hello"},
			map[string]interface{}{"type": "text", "text": "world"},
		},
	})
	out, err := CallMCPTool(map[string]interface{}{"transport": "http", "url": server.URL}, nil, "echo", map[string]interface{}{"msg": "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hello\nworld" {
		t.Fatalf("output = %q, want %q", out, "hello\nworld")
	}
}

// TestCallMCPToolJSONFallback guards that a result without content blocks comes
// back as JSON rather than being dropped.
func TestCallMCPToolJSONFallback(t *testing.T) {
	server := mcpStubServer(t, map[string]interface{}{"value": 42})
	out, err := CallMCPTool(map[string]interface{}{"transport": "http", "url": server.URL}, nil, "get", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "42") {
		t.Fatalf("fallback output = %q, want the JSON result", out)
	}
}

// TestCallMCPToolErrorsOnIsError guards that a failed MCP call is an error, not
// a success the caller could mistake for a result.
func TestCallMCPToolErrorsOnIsError(t *testing.T) {
	server := mcpStubServer(t, map[string]interface{}{
		"isError": true,
		"content": []interface{}{map[string]interface{}{"type": "text", "text": "boom"}},
	})
	_, err := CallMCPTool(map[string]interface{}{"transport": "http", "url": server.URL}, nil, "fail", nil)
	if err == nil || !strings.Contains(err.Error(), "isError") {
		t.Fatalf("expected an isError error, got %v", err)
	}
}

// TestCallMCPToolRequiresName guards the input validation.
func TestCallMCPToolRequiresName(t *testing.T) {
	if _, err := CallMCPTool(map[string]interface{}{"transport": "http", "url": "https://example.com/mcp"}, nil, "   ", nil); err == nil {
		t.Fatal("an empty tool name must be rejected")
	}
}
