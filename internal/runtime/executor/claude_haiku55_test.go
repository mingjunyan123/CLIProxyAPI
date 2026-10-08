package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	"github.com/tidwall/gjson"
)

func TestClaudeHaiku55Thinking(t *testing.T) {
	modelInfo := registry.LookupStaticModelInfoByChannel("claude-haiku-5-5", "claude")
	if modelInfo == nil || modelInfo.ContextLength != 1000000 || modelInfo.MaxCompletionTokens != 128000 {
		t.Fatalf("Haiku 5.5 catalog entry must advertise 1M context and 128K output: %+v", modelInfo)
	}
	for _, tc := range []struct {
		name, model, body, wantType, wantEffort string
	}{
		{"high", "claude-haiku-5-5", `{"thinking":{"type":"adaptive"},"output_config":{"effort":"high"}}`, "adaptive", "high"},
		{"xhigh", "claude-haiku-5-5", `{"thinking":{"type":"adaptive"},"output_config":{"effort":"xhigh"}}`, "adaptive", "xhigh"},
		{"max", "claude-haiku-5-5", `{"thinking":{"type":"adaptive"},"output_config":{"effort":"max"}}`, "adaptive", "max"},
		{"manual budget converts to adaptive", "claude-haiku-5-5", `{"thinking":{"type":"enabled","budget_tokens":8192}}`, "adaptive", "medium"},
		{"auto preserves provider default", "claude-haiku-5-5(auto)", `{}`, "adaptive", ""},
		{"none", "claude-haiku-5-5(none)", `{}`, "disabled", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, errApply := thinking.ApplyThinking([]byte(tc.body), tc.model, "claude", "claude", "claude")
			if errApply != nil {
				t.Fatal(errApply)
			}
			if got := gjson.GetBytes(out, "thinking.type").String(); got != tc.wantType {
				t.Fatalf("thinking.type = %q, want %q: %s", got, tc.wantType, out)
			}
			if got := gjson.GetBytes(out, "output_config.effort").String(); got != tc.wantEffort {
				t.Fatalf("effort = %q, want %q: %s", got, tc.wantEffort, out)
			}
			if gjson.GetBytes(out, "thinking.budget_tokens").Exists() {
				t.Fatalf("manual thinking budget must be removed: %s", out)
			}
		})
	}
}

func TestClaudeHaiku55PayloadOverrideRemainsFinal(t *testing.T) {
	var seenBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","model":"claude-haiku-5-5","role":"assistant","content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	cfg := &config.Config{Payload: config.PayloadConfig{Override: []config.PayloadRule{{
		Models: []config.PayloadModelRule{{Name: "claude-haiku-5-5"}},
		Params: map[string]any{"temperature": 0.5, "top_p": 0.95, "top_k": 40},
	}}}}
	auth := &cliproxyauth.Auth{
		ID:       "haiku55-payload-override",
		Metadata: claudeOAuthTestMetadata(),
		Attributes: map[string]string{
			"api_key":  "sk-ant-oat-haiku55-payload-override",
			"base_url": server.URL,
		},
	}
	_, errExecute := NewClaudeExecutor(cfg).Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "claude-haiku-5-5",
		Payload: []byte(`{"model":"claude-haiku-5-5","max_tokens":1000,"thinking":{"type":"adaptive"},"output_config":{"effort":"medium"},"messages":[{"role":"user","content":"explain sorting"}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatClaude})
	if errExecute != nil {
		t.Fatal(errExecute)
	}
	for field, want := range map[string]float64{"temperature": 0.5, "top_p": 0.95, "top_k": 40} {
		if got := gjson.GetBytes(seenBody, field); !got.Exists() || got.Num != want {
			t.Fatalf("payload override %s = %q, want %v", field, got.Raw, want)
		}
	}
}

func TestClaudeHaiku55EffortBeta(t *testing.T) {
	for _, model := range []string{"claude-haiku-5-5", "anthropic/claude-haiku-5-5", "claude-haiku-4-5-20251001"} {
		body := []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"explain sorting"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"medium"}}`, model))
		betas := strings.Split(claudeCodeCLIBetas(body, nil, true), ",")
		found := false
		for _, beta := range betas {
			if beta == claudeEffortBeta {
				found = true
			}
		}
		if want := model != "claude-haiku-4-5-20251001"; found != want {
			t.Fatalf("model %s: effort beta present = %v, want %v", model, found, want)
		}
	}
}

func TestClaudeHaiku55Sampling(t *testing.T) {
	for _, nativeOwned := range []bool{false, true} {
		for _, tc := range []struct {
			name, sampling, thinking, wantTemperature, wantTopP string
		}{
			{"implicit thinking rejects sampling", `"temperature":0.5,"top_p":1,"top_k":40,`, "", "", ""},
			{"disabled thinking rejects sampling", `"temperature":0.5,"top_p":0.95,"top_k":40,`, `"thinking":{"type":"disabled"},`, "", ""},
			{"adaptive thinking rejects top_p", `"top_p":1,"top_k":40,`, `"thinking":{"type":"adaptive"},`, "", ""},
			{"default temperature", `"temperature":1,"top_k":40,`, "", "1", ""},
			{"default top_p", `"top_p":0.99,"top_k":40,`, "", "", "0.99"},
			{"both defaults keep temperature", `"temperature":1,"top_p":0.99,"top_k":40,`, "", "1", ""},
		} {
			t.Run(fmt.Sprintf("%s/native=%v", tc.name, nativeOwned), func(t *testing.T) {
				body := []byte(`{"model":"claude-haiku-5-5",` + tc.sampling + tc.thinking + `"max_tokens":1000}`)
				out := normalizeClaudeSamplingForUpstream(body, nativeOwned)
				wantTemperature, wantTopP := tc.wantTemperature, tc.wantTopP
				if !nativeOwned {
					wantTemperature, wantTopP = "", ""
				}
				if got := gjson.GetBytes(out, "temperature").Raw; got != wantTemperature {
					t.Fatalf("temperature = %q, want %q", got, wantTemperature)
				}
				if got := gjson.GetBytes(out, "top_p").Raw; got != wantTopP {
					t.Fatalf("top_p = %q, want %q", got, wantTopP)
				}
				if gjson.GetBytes(out, "top_k").Exists() {
					t.Fatalf("top_k must be removed: %s", out)
				}
			})
		}
	}
}
