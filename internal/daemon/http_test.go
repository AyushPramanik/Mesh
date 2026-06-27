package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AyushPramanik/mesh/internal/store"
)

func newTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	st, err := store.Open(context.Background(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return &Daemon{Store: st, ghStatus: "not configured", processQueue: false}
}

func TestHealthz_OK(t *testing.T) {
	d := newTestDaemon(t)
	rec := httptest.NewRecorder()
	d.handleHealthz(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "ok", body["status"])
}

func TestReadyz_ReadyWhenDBAlive(t *testing.T) {
	d := newTestDaemon(t)
	rec := httptest.NewRecorder()
	d.handleReadyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "ready", body["status"])
	assert.Equal(t, false, body["queueProcessing"])
	assert.Equal(t, "not configured", body["github"])
}

func TestReadyz_UnavailableWhenDBClosed(t *testing.T) {
	d := newTestDaemon(t)
	require.NoError(t, d.Store.Close()) // simulate a dead database

	rec := httptest.NewRecorder()
	d.handleReadyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "not ready", body["status"])
}
