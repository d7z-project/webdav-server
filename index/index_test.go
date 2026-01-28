package index

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"code.d7z.net/packages/webdav-server/common"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func TestWithIndexRedirect(t *testing.T) {
	// Setup
	cfg := &common.Config{
		Webdav: common.ConfigWebdav{Enabled: true},
	}
	ctx := &common.FsContext{Config: cfg}
	r := chi.NewMux()
	WithIndex(ctx, r)

	// Test Case 1: Login without return URL (should 401 if not auth)
	req1 := httptest.NewRequest("GET", "/?login=true", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusUnauthorized, w1.Code)

	// Test Case 2: Login with return URL but not authenticated (should 401)
	req2 := httptest.NewRequest("GET", "/?login=true&return=/foo", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusUnauthorized, w2.Code)

	// Test Case 3: Login with return URL and authenticated (should 302 redirect)
	req3 := httptest.NewRequest("GET", "/?login=true&return=/foo", nil)
	req3.SetBasicAuth("admin", "123456") // Credentials don't matter for this unit test mock, just that BasicAuth is present
	// Note: basic auth verification logic in index.go depends on request.BasicAuth() returning ok.
	// However, the actual user validation logic seems to rely on the fact that if BasicAuth is present
	// and valid (handled by middleware or just parsed here).
	// In index.go: `if user, _, ok := request.BasicAuth(); !ok || user == "guest" { ... }`
	// So we just need to provide ANY basic auth that is not "guest".

	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	assert.Equal(t, http.StatusFound, w3.Code)
	assert.Equal(t, "/foo", w3.Header().Get("Location"))
}
