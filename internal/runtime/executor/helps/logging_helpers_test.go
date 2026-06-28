package helps

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
)

func TestRecordAPIResponseMetadataStoresHeadersWhenRequestLogDisabled(t *testing.T) {
	ctx := logging.WithResponseHeadersHolder(context.Background())
	headers := http.Header{}
	headers.Add("X-Upstream-Request-Id", "upstream-req-1")

	RecordAPIResponseMetadata(ctx, &config.Config{}, http.StatusOK, headers)
	headers.Set("X-Upstream-Request-Id", "mutated")

	got := logging.GetResponseHeaders(ctx)
	if got.Get("X-Upstream-Request-Id") != "upstream-req-1" {
		t.Fatalf("response header = %q, want %q", got.Get("X-Upstream-Request-Id"), "upstream-req-1")
	}
}

func TestRecordAPIRequestWritesClaudeOAuthOutboundLog(t *testing.T) {
	logRoot := t.TempDir()
	t.Setenv("WRITABLE_PATH", logRoot)

	ctx := logging.WithRequestID(context.Background(), "req-1")
	RecordAPIRequest(ctx, &config.Config{SDKConfig: config.SDKConfig{RequestLog: true}}, UpstreamRequestLog{
		URL:       "https://api.anthropic.com/v1/messages?beta=true",
		Method:    http.MethodPost,
		Headers:   http.Header{"X-Test": []string{"value"}},
		Body:      []byte(`{"model":"claude-sonnet-4"}`),
		Provider:  "claude",
		AuthID:    "claude-auth",
		AuthType:  "oauth",
		AuthValue: "user@example.com",
	})

	matches, err := filepath.Glob(filepath.Join(logRoot, "logs", "claude-oauth", "user-example.com", "*.log"))
	if err != nil {
		t.Fatalf("glob outbound logs: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("outbound log files = %d, want 1", len(matches))
	}

	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read outbound log: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"=== API REQUEST 1 ===",
		"Upstream URL: https://api.anthropic.com/v1/messages?beta=true",
		"HTTP Method: POST",
		"Auth: provider=claude, auth_id=claude-auth, type=oauth",
		`{"model":"claude-sonnet-4"}`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("outbound log missing %q:\n%s", want, content)
		}
	}
	for _, notWant := range []string{"=== REQUEST INFO ===", "=== API RESPONSE", "=== RESPONSE ==="} {
		if strings.Contains(content, notWant) {
			t.Fatalf("outbound log should not contain %q:\n%s", notWant, content)
		}
	}
}

func TestRecordAPIRequestSkipsNonClaudeOAuthOutboundLog(t *testing.T) {
	logRoot := t.TempDir()
	t.Setenv("WRITABLE_PATH", logRoot)

	cfg := &config.Config{SDKConfig: config.SDKConfig{RequestLog: true}}
	RecordAPIRequest(context.Background(), cfg, UpstreamRequestLog{
		URL:      "https://api.anthropic.com/v1/messages",
		Method:   http.MethodPost,
		Body:     []byte(`{}`),
		Provider: "claude",
		AuthID:   "claude-api-key",
		AuthType: "api_key",
	})
	RecordAPIRequest(context.Background(), cfg, UpstreamRequestLog{
		URL:       "https://generativelanguage.googleapis.com/v1beta/models",
		Method:    http.MethodPost,
		Body:      []byte(`{}`),
		Provider:  "gemini",
		AuthID:    "gemini-oauth",
		AuthType:  "oauth",
		AuthValue: "user@example.com",
	})

	if _, err := os.Stat(filepath.Join(logRoot, "logs", "claude-oauth")); !os.IsNotExist(err) {
		t.Fatalf("claude oauth log dir exists or stat failed: %v", err)
	}
}
