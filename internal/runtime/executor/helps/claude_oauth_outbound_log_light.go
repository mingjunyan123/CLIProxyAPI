package helps

import (
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
)

func writeLightweightClaudeOAuthOutboundLog(ctx context.Context, cfg *config.Config, info UpstreamRequestLog, index int) (err error) {
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
