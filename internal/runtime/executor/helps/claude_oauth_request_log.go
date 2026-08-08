package helps

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
)

func shouldWriteClaudeOAuthOutboundLog(info UpstreamRequestLog) bool {
	return strings.EqualFold(strings.TrimSpace(info.Provider), "claude") &&
		strings.EqualFold(strings.TrimSpace(info.AuthType), "oauth")
}

func writeClaudeOAuthOutboundLog(ctx context.Context, cfg *config.Config, info UpstreamRequestLog, index int) error {
	builder := newAPIRequestLogBuilder(index, info, time.Now())
	if len(info.Body) > 0 {
		builder.Write(info.Body)
	} else {
		builder.WriteString("<empty>")
	}
	builder.WriteString("\n\n")

	account := strings.TrimSpace(info.AuthValue)
	if account == "" {
		account = strings.TrimSpace(info.AuthID)
	}
	if account == "" {
		account = "unknown-account"
	}

	logDir := filepath.Join(logging.ResolveLogDirectory(cfg), "claude-oauth", sanitizeLogPathPart(account))
	if errMkdir := os.MkdirAll(logDir, 0755); errMkdir != nil {
		return fmt.Errorf("create claude oauth request log dir: %w", errMkdir)
	}

	filePath := filepath.Join(logDir, outboundRequestLogFilename(ctx, info))
	if errWrite := writeClaudeOAuthGzipLog(filePath, builder.String()); errWrite != nil {
		return fmt.Errorf("write claude oauth request log: %w", errWrite)
	}
	return nil
}

func writeClaudeOAuthGzipLog(filePath, content string) error {
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	if _, errWrite := io.WriteString(gzipWriter, content); errWrite != nil {
		_ = gzipWriter.Close()
		return fmt.Errorf("compress log: %w", errWrite)
	}
	if errClose := gzipWriter.Close(); errClose != nil {
		return fmt.Errorf("finish log compression: %w", errClose)
	}
	if errWrite := os.WriteFile(filePath, compressed.Bytes(), 0644); errWrite != nil {
		_ = os.Remove(filePath)
		return errWrite
	}
	return nil
}

func outboundRequestLogFilename(ctx context.Context, info UpstreamRequestLog) string {
	requestID := logging.GetRequestID(ctx)
	if requestID == "" {
		requestID = logging.GenerateRequestID()
	}
	pathPart := "root"
	if parsed, errParse := url.Parse(strings.TrimSpace(info.URL)); errParse == nil {
		pathPart = strings.Trim(parsed.Path, "/")
	} else if trimmed := strings.TrimSpace(info.URL); trimmed != "" {
		pathPart = trimmed
	}
	if pathPart == "" {
		pathPart = "root"
	}
	timestamp := time.Now().Format("2006-01-02T150405.000000000")
	return fmt.Sprintf("api-request-%s-%s-%s.log.gz", sanitizeLogPathPart(pathPart), timestamp, sanitizeLogPathPart(requestID))
}

func sanitizeLogPathPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '.', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteByte('-')
		}
	}
	out := strings.Trim(builder.String(), ".-_")
	if out == "" {
		return "unknown"
	}
	if len(out) > 120 {
		out = strings.Trim(out[:120], ".-_")
		if out == "" {
			return "unknown"
		}
	}
	return out
}
