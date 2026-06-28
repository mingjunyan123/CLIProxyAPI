package helps

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

func TestRequestLoggingDoesNotMarkUpstreamAttempt(t *testing.T) {
	tests := []struct {
		name   string
		record func(context.Context)
	}{
		{
			name: "HTTP",
			record: func(ctx context.Context) {
				RecordAPIRequest(ctx, &config.Config{}, UpstreamRequestLog{URL: "https://api.example.com", Method: http.MethodPost})
			},
		},
		{
			name: "websocket",
			record: func(ctx context.Context) {
				RecordAPIWebsocketRequest(ctx, &config.Config{}, UpstreamRequestLog{URL: "wss://api.example.com", Method: "WEBSOCKET"})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := cliproxyexecutor.WithUpstreamAttemptTracker(context.Background())
			test.record(ctx)
			if cliproxyexecutor.UpstreamAttempted(ctx) {
				t.Fatal("request logging marked an upstream attempt before transport")
			}
		})
	}
}

func TestRecordAPIRequestClonesDeferredBodyWhenRequestLogDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ctx := context.WithValue(context.Background(), "gin", ginCtx)
	body := []byte(`{"model":"original"}`)

	RecordAPIRequest(ctx, &config.Config{}, UpstreamRequestLog{
		URL:    "https://api.example.com/v1/responses",
		Method: http.MethodPost,
		Body:   body,
	})
	body[10] = 'X'

	value, exists := ginCtx.Get(logging.DeferredAPIRequestContextKey)
	if !exists {
		t.Fatal("deferred API request was not captured")
	}
	requests, ok := value.([]logging.DeferredAPIRequest)
	if !ok || len(requests) != 1 {
		t.Fatalf("deferred API requests = %#v, want one request", value)
	}
	captured := string(requests[0]())
	if !strings.Contains(captured, `{"model":"original"}`) {
		t.Fatalf("captured API request = %q, want original body", captured)
	}
}

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

func TestAPIResponseAttemptsAreSeparated(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name              string
		fileBacked        bool
		firstResponseBody []byte
	}{
		{name: "memory backed error"},
		{name: "file backed error", fileBacked: true},
		{name: "memory backed partial body", firstResponseBody: []byte("partial")},
		{name: "file backed partial body", fileBacked: true, firstResponseBody: []byte("partial")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ginCtx, _ := gin.CreateTestContext(recorder)
			var responseSource *logging.FileBodySource
			if tt.fileBacked {
				var errSource error
				responseSource, errSource = logging.NewFileBodySourceInDir(t.TempDir(), "api-response")
				if errSource != nil {
					t.Fatalf("NewFileBodySourceInDir: %v", errSource)
				}
				t.Cleanup(func() {
					if errCleanup := responseSource.Cleanup(); errCleanup != nil {
						t.Errorf("Cleanup: %v", errCleanup)
					}
				})
				ginCtx.Set(logging.APIResponseSourceContextKey, responseSource)
			}

			ctx := context.WithValue(context.Background(), "gin", ginCtx)
			cfg := &config.Config{SDKConfig: config.SDKConfig{RequestLog: true}}
			RecordAPIRequest(ctx, cfg, UpstreamRequestLog{URL: "https://api.example.com/first", Method: http.MethodPost})
			if len(tt.firstResponseBody) > 0 {
				AppendAPIResponseChunk(ctx, cfg, tt.firstResponseBody)
			} else {
				RecordAPIResponseError(ctx, cfg, errors.New("EOF"))
			}
			RecordAPIRequest(ctx, cfg, UpstreamRequestLog{URL: "https://api.example.com/second", Method: http.MethodPost})
			RecordAPIResponseError(ctx, cfg, errors.New("retry failed"))

			var response []byte
			if responseSource != nil {
				var errBytes error
				response, errBytes = responseSource.Bytes()
				if errBytes != nil {
					t.Fatalf("responseSource.Bytes: %v", errBytes)
				}
			} else {
				value, exists := ginCtx.Get(apiResponseKey)
				if !exists {
					t.Fatal("API_RESPONSE was not captured")
				}
				response, _ = value.([]byte)
			}

			previousEnd := "Error: EOF"
			if len(tt.firstResponseBody) > 0 {
				previousEnd = string(tt.firstResponseBody)
			}
			wantBoundary := previousEnd + "\n\n=== API RESPONSE 2 ==="
			if !strings.Contains(string(response), wantBoundary) {
				t.Fatalf("API response attempts are not separated by one blank line:\n%q\nwant boundary %q", response, wantBoundary)
			}
		})
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
