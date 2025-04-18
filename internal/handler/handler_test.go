package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestHello(t *testing.T) {
	logger := zap.NewNop()
	h, err := New(
		10*time.Second,
		10*time.Second,
		10*time.Second,
		true,
		nil,
		nil,
		0,
		logger,
	)
	assert.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	rec := httptest.NewRecorder()

	handler := h.Hello()
	handler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "OK\n", rec.Body.String())
}
