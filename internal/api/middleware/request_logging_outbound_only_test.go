package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
)

func TestRequestLoggingMiddlewareSkipsFullLogWhenEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logsDir := t.TempDir()
	logger := logging.NewFileRequestLogger(true, logsDir, "", 10)
	sourceAttached := false

	router := gin.New()
	router.Use(RequestLoggingMiddleware(logger))
	router.POST("/v1/messages", func(c *gin.Context) {
		_, sourceAttached = c.Get(logging.APIRequestSourceContextKey)
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if sourceAttached {
		t.Fatal("full request log source was attached")
	}
	entries, errReadDir := os.ReadDir(logsDir)
	if errReadDir != nil {
		t.Fatalf("read logs dir: %v", errReadDir)
	}
	if len(entries) != 0 {
		t.Fatalf("full request log files = %d, want 0", len(entries))
	}
}
