package helps

import (
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	log "github.com/sirupsen/logrus"
)

// captureClaudeOAuthOutboundRequest is the only fork hook RecordAPIRequest
// should call. It writes Claude OAuth upstream payloads when
// claude-oauth-outbound-log is enabled and does not change request-log.
func captureClaudeOAuthOutboundRequest(cfg *config.Config, info UpstreamRequestLog, ginCtx *gin.Context) {
	if cfg == nil || !cfg.ClaudeOAuthOutboundLog || !shouldWriteClaudeOAuthOutboundLog(info) {
		return
	}
	index := len(getAttempts(ginCtx)) + 1
	if errWrite := writeLightweightClaudeOAuthOutboundLog(cfg, info, index); errWrite != nil {
		log.WithError(errWrite).Warn("failed to write claude oauth outbound request log")
	}
}

func shouldWriteClaudeOAuthOutboundLog(info UpstreamRequestLog) bool {
	return strings.EqualFold(strings.TrimSpace(info.Provider), "claude") &&
		strings.EqualFold(strings.TrimSpace(info.AuthType), "oauth")
}

func claudeOAuthOutboundAccount(info UpstreamRequestLog) string {
	account := strings.TrimSpace(info.AuthValue)
	if account == "" {
		account = strings.TrimSpace(info.AuthID)
	}
	if account == "" {
		return "unknown-account"
	}
	return account
}

func claudeOAuthOutboundLogFile(cfg *config.Config, info UpstreamRequestLog) (string, error) {
	logDir := filepath.Join(logging.ResolveLogDirectory(cfg), "claude-oauth", sanitizeLogPathPart(claudeOAuthOutboundAccount(info)))
	if errMkdir := os.MkdirAll(logDir, 0755); errMkdir != nil {
		return "", fmt.Errorf("create claude oauth request log dir: %w", errMkdir)
	}
	return filepath.Join(logDir, outboundRequestLogFilename(info)), nil
}

func writeLightweightClaudeOAuthOutboundLog(cfg *config.Config, info UpstreamRequestLog, index int) (err error) {
	filePath, errPath := claudeOAuthOutboundLogFile(cfg, info)
	if errPath != nil {
		return errPath
	}
	file, errCreate := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if errCreate != nil {
		return fmt.Errorf("create claude oauth request log: %w", errCreate)
	}

	defer func() {
		if errClose := file.Close(); err == nil && errClose != nil {
			err = fmt.Errorf("close claude oauth request log: %w", errClose)
		}
		if err != nil {
			_ = os.Remove(filePath)
		}
	}()

	gzipWriter, errGzip := gzip.NewWriterLevel(file, gzip.BestSpeed)
	if errGzip != nil {
		return fmt.Errorf("create gzip writer: %w", errGzip)
	}
	prefix := newAPIRequestLogBuilder(index, info, time.Now()).String()
	if _, errWrite := gzipWriter.Write([]byte(prefix)); errWrite != nil {
		_ = gzipWriter.Close()
		return fmt.Errorf("write log prefix: %w", errWrite)
	}
	if len(info.Body) > 0 {
		if _, errWrite := gzipWriter.Write(info.Body); errWrite != nil {
			_ = gzipWriter.Close()
			return fmt.Errorf("write log body: %w", errWrite)
		}
	} else if _, errWrite := gzipWriter.Write([]byte("<empty>")); errWrite != nil {
		_ = gzipWriter.Close()
		return fmt.Errorf("write empty log body: %w", errWrite)
	}
	if _, errWrite := gzipWriter.Write([]byte("\n\n")); errWrite != nil {
		_ = gzipWriter.Close()
		return fmt.Errorf("write log terminator: %w", errWrite)
	}
	if errClose := gzipWriter.Close(); errClose != nil {
		return fmt.Errorf("finish log compression: %w", errClose)
	}
	return nil
}

func outboundRequestLogFilename(info UpstreamRequestLog) string {
	entrypoint := "cli"
	if parsed, _ := parseClaudeCodeUserAgentDetails(headerValue(info.Headers, "User-Agent")); parsed != "" {
		entrypoint = parsed
	}
	return sanitizeLogPathPart(entrypoint) + "-" + time.Now().Format("20060102-150405.000000") + ".gz"
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
