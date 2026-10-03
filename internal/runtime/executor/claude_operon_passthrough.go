package executor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	log "github.com/sirupsen/logrus"
)

// operonSkipHeaders are transport fields and inbound proxy credentials.
// The selected Claude OAuth token is applied after these are dropped.
var operonSkipHeaders = map[string]struct{}{
	"authorization":       {},
	"connection":          {},
	"content-length":      {},
	"host":                {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"x-api-key":           {},
	"x-goog-api-key":      {},
}

func operonPassthroughSeen(ctx context.Context, opts cliproxyexecutor.Options) bool {
	surface, ua := operonIdentity(opts.Headers)
	if surface == "" || ua == "" {
		ginHeaders := requestHeaderFromGin(ctx)
		if surface == "" {
			surface = headerValue(ginHeaders, "X-Anthropic-Surface")
		}
		if ua == "" {
			ua = headerValue(ginHeaders, "User-Agent")
		}
	}
	return strings.EqualFold(strings.TrimSpace(surface), "operon") &&
		strings.HasPrefix(strings.TrimSpace(ua), "ks/JS")
}

func operonIdentity(headers http.Header) (surface, userAgent string) {
	return headerValue(headers, "X-Anthropic-Surface"), headerValue(headers, "User-Agent")
}

func requestHeaderFromGin(ctx context.Context) http.Header {
	ginCtx, ok := ctx.Value("gin").(*gin.Context)
	if !ok || ginCtx == nil || ginCtx.Request == nil {
		return nil
	}
	return ginCtx.Request.Header
}

func headerValue(headers http.Header, name string) string {
	if headers == nil {
		return ""
	}
	if value := headers.Get(name); value != "" {
		return value
	}
	for key, values := range headers {
		if strings.EqualFold(key, name) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

func (e *ClaudeExecutor) operonUpstreamBody(req cliproxyexecutor.Request) (baseModel, upstreamModel string, body []byte) {
	baseModel = thinking.ParseSuffix(req.Model).ModelName
	upstreamModel = e.upstreamModel(baseModel)
	body = req.Payload
	if strings.TrimSpace(upstreamModel) != "" {
		body = helps.SetStringIfDifferent(body, "model", upstreamModel)
	}
	return baseModel, upstreamModel, body
}

func (e *ClaudeExecutor) prepareOperonOAuthRequest(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*http.Request, string, string, bool, error) {
	if !operonPassthroughSeen(ctx, opts) {
		return nil, "", "", false, nil
	}
	incoming := resolveIncomingClaudeHeaders(ctx, opts.Headers)
	apiKey, baseURL := claudeCreds(auth)
	if !isClaudeOAuthToken(apiKey) {
		return nil, "", "", false, nil
	}
	baseModel, upstreamModel, body := e.operonUpstreamBody(req)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, "", "", true, err
	}
	applyOperonOutboundHeaders(httpReq, incoming, apiKey)
	authID, authLabel, authType, authValue := claudeAuthLogIdentity(auth)
	helps.RecordAPIRequest(ctx, e.cfg, helps.UpstreamRequestLog{
		URL:       httpReq.URL.String(),
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      body,
		Provider:  e.upstreamRequestLogProvider(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})
	return httpReq, baseModel, upstreamModel, true, nil
}

func applyOperonOutboundHeaders(req *http.Request, src http.Header, apiKey string) {
	dst := make(http.Header)
	hasContentType := false
	for key, values := range src {
		if _, skip := operonSkipHeaders[strings.ToLower(key)]; skip {
			continue
		}
		if strings.EqualFold(key, "Content-Type") {
			hasContentType = true
		}
		dst[key] = append([]string(nil), values...)
	}
	if !hasContentType {
		dst["Content-Type"] = []string{"application/json"}
	}
	dst["Authorization"] = []string{"Bearer " + apiKey}
	req.Header = dst
}

func (e *ClaudeExecutor) forwardOperonOAuth(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, handled bool, err error) {
	httpReq, baseModel, upstreamModel, handled, errPrepare := e.prepareOperonOAuthRequest(ctx, auth, req, opts)
	if !handled || errPrepare != nil {
		return cliproxyexecutor.Response{}, handled, errPrepare
	}
	reporter := helps.NewExecutorUsageReporter(ctx, e, baseModel, auth)
	defer reporter.TrackFailure(ctx, &err)
	if upstreamModel != baseModel {
		reporter.SetUpstreamModel(upstreamModel)
	}
	httpClient := reporter.TrackHTTPClient(helps.NewUtlsHTTPClient(ctx, e.cfg, auth, 0))
	httpResp, errDo := doClaudeUpstreamRequest(httpClient, httpReq)
	if errDo != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, errDo)
		return cliproxyexecutor.Response{}, true, errDo
	}
	helps.RecordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())
	decoded, errDecode := decodeResponseBody(httpResp.Body, claudeResponseContentEncoding(httpResp.Header))
	if errDecode != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, errDecode)
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("response body close error: %v", errClose)
		}
		return cliproxyexecutor.Response{}, true, errDecode
	}
	defer func() {
		if errClose := decoded.Close(); errClose != nil {
			log.Errorf("response body close error: %v", errClose)
		}
	}()
	data, errRead := io.ReadAll(decoded)
	if errRead != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, errRead)
		return cliproxyexecutor.Response{}, true, errRead
	}
	helps.AppendAPIResponseChunk(ctx, e.cfg, data)
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		helps.LogWithRequestID(ctx).Debugf("request error, error status: %d, error message: %s", httpResp.StatusCode, helps.SummarizeErrorBody(httpResp.Header.Get("Content-Type"), data))
		return cliproxyexecutor.Response{}, true, classifyClaudeUpstreamErrorWithCooling(httpResp.StatusCode, httpResp.Header, data, e.modelLevelCooling())
	}
	reporter.ObserveResponseModel(data)
	reporter.Publish(ctx, helps.ParseClaudeUsage(data))
	return cliproxyexecutor.Response{Payload: data, Headers: httpResp.Header.Clone()}, true, nil
}

func (e *ClaudeExecutor) forwardOperonOAuthStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (result *cliproxyexecutor.StreamResult, handled bool, err error) {
	httpReq, baseModel, upstreamModel, handled, errPrepare := e.prepareOperonOAuthRequest(ctx, auth, req, opts)
	if !handled || errPrepare != nil {
		return nil, handled, errPrepare
	}
	reporter := helps.NewExecutorUsageReporter(ctx, e, baseModel, auth)
	defer reporter.TrackFailure(ctx, &err)
	if upstreamModel != baseModel {
		reporter.SetUpstreamModel(upstreamModel)
	}
	httpClient := reporter.TrackHTTPClient(helps.NewUtlsHTTPClient(ctx, e.cfg, auth, 0))
	httpResp, errDo := doClaudeUpstreamRequest(httpClient, httpReq)
	if errDo != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, errDo)
		return nil, true, errDo
	}
	helps.RecordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		errBody, decErr := decodeResponseBody(httpResp.Body, claudeResponseContentEncoding(httpResp.Header))
		if decErr != nil {
			helps.RecordAPIResponseError(ctx, e.cfg, decErr)
			if errClose := httpResp.Body.Close(); errClose != nil {
				log.Errorf("response body close error: %v", errClose)
			}
			return nil, true, decErr
		}
		b, readErr := io.ReadAll(errBody)
		if errClose := errBody.Close(); errClose != nil {
			log.Errorf("response body close error: %v", errClose)
		}
		if readErr != nil {
			helps.RecordAPIResponseError(ctx, e.cfg, readErr)
			b = []byte(fmt.Sprintf("failed to read error response body: %v", readErr))
		}
		helps.AppendAPIResponseChunk(ctx, e.cfg, b)
		return nil, true, classifyClaudeUpstreamErrorWithCooling(httpResp.StatusCode, httpResp.Header, b, e.modelLevelCooling())
	}
	decoded, errDecode := decodeResponseBody(httpResp.Body, claudeResponseContentEncoding(httpResp.Header))
	if errDecode != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, errDecode)
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("response body close error: %v", errClose)
		}
		return nil, true, errDecode
	}
	out := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(out)
		defer func() {
			if errClose := decoded.Close(); errClose != nil {
				log.Errorf("response body close error: %v", errClose)
			}
		}()
		var streamUsage helps.StreamUsageBuffer
		defer streamUsage.Publish(ctx, reporter)
		scanner := bufio.NewScanner(decoded)
		scanner.Buffer(nil, 52_428_800)
		var event bytes.Buffer
		var upstreamMessageID string
		upstreamCompleted := false
		flushEvent := func() bool {
			if event.Len() == 0 {
				return true
			}
			cloned := bytes.Clone(event.Bytes())
			event.Reset()
			select {
			case out <- cliproxyexecutor.StreamChunk{Payload: cloned}:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for scanner.Scan() {
			line := scanner.Bytes()
			observeClaudeStreamLine(line, &upstreamMessageID, &upstreamCompleted)
			helps.AppendAPIResponseChunk(ctx, e.cfg, line)
			reporter.ObserveResponseModel(line)
			streamUsage.ObserveClaudeStream(line)
			event.Write(line)
			event.WriteByte('\n')
			if len(bytes.TrimSpace(line)) == 0 {
				if !flushEvent() {
					return
				}
				if upstreamCompleted {
					break
				}
			}
		}
		if !flushEvent() {
			return
		}
		if upstreamCompleted {
			return
		}
		if errScan := scanner.Err(); errScan != nil {
			helps.RecordAPIResponseError(ctx, e.cfg, errScan)
			streamUsage.PublishFailure(ctx, reporter, errScan)
			select {
			case out <- cliproxyexecutor.StreamChunk{Err: errScan}:
			case <-ctx.Done():
			}
		}
	}()
	return &cliproxyexecutor.StreamResult{Headers: httpResp.Header.Clone(), Chunks: out}, true, nil
}
