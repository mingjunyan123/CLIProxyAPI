package helps

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
)

func TestOutboundRequestLogFilename(t *testing.T) {
	got := outboundRequestLogFilename(UpstreamRequestLog{
		Headers: http.Header{"User-Agent": []string{"claude-cli/2.1.258 (external, local-agent, agent-sdk/0.3.258)"}},
	})
	if !regexp.MustCompile(`^local-agent-\d{8}-\d{6}\.\d{6}\.gz$`).MatchString(got) {
		t.Fatalf("filename = %q, want local-agent-YYYYMMDD-HHMMSS.ffffff.gz", got)
	}
	got = outboundRequestLogFilename(UpstreamRequestLog{})
	if !regexp.MustCompile(`^cli-\d{8}-\d{6}\.\d{6}\.gz$`).MatchString(got) {
		t.Fatalf("default filename = %q, want cli-YYYYMMDD-HHMMSS.ffffff.gz", got)
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

	matches, err := filepath.Glob(filepath.Join(logRoot, "logs", "claude-oauth", "user-example.com", "*.gz"))
	if err != nil {
		t.Fatalf("glob outbound logs: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("outbound log files = %d, want 1", len(matches))
	}
	if !regexp.MustCompile(`^cli-\d{8}-\d{6}\.\d{6}\.gz$`).MatchString(filepath.Base(matches[0])) {
		t.Fatalf("outbound log name = %q, want cli-YYYYMMDD-HHMMSS.ffffff.gz", filepath.Base(matches[0]))
	}

	file, err := os.Open(matches[0])
	if err != nil {
		t.Fatalf("open outbound log: %v", err)
	}
	defer func() {
		if errClose := file.Close(); errClose != nil {
			t.Errorf("close outbound log: %v", errClose)
		}
	}()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer func() {
		if errClose := gzipReader.Close(); errClose != nil {
			t.Errorf("close gzip reader: %v", errClose)
		}
	}()
	data, err := io.ReadAll(gzipReader)
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
