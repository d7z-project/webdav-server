package index

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"code.d7z.net/packages/webdav-server/common"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func TestWithIndexRoutes(t *testing.T) {
	// Setup minimal context
	cfg := &common.Config{
		Bind: ":8080",
		Pools: map[string]common.ConfigPool{
			"default": {Path: ".", DefaultPerm: "r"},
		},
		Users: map[string]common.ConfigUser{
			"testuser": {Password: "pass"},
		},
	}
	ctx, err := common.NewContext(context.Background(), cfg)
	assert.NoError(t, err)

	r := chi.NewMux()
	WithIndex(ctx, r)

	// Test 1: Legacy login param redirects to /login
	req1 := httptest.NewRequest("GET", "/?login=true", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusFound, w1.Code)
	assert.Equal(t, "/login", w1.Header().Get("Location"))

	// Test 2: Login page renders
	req2 := httptest.NewRequest("GET", "/login", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)
	assert.Contains(t, w2.Body.String(), "用户登录")

	// Test 3: POST /login invalid
	req3 := httptest.NewRequest("POST", "/login", strings.NewReader("username=bad&password=bad"))
	req3.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	assert.Equal(t, http.StatusUnauthorized, w3.Code)

	// Test 4: POST /login valid
	req4 := httptest.NewRequest("POST", "/login", strings.NewReader("username=testuser&password=pass&return=/home"))
	req4.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	assert.Equal(t, http.StatusFound, w4.Code)
	assert.Equal(t, "/home", w4.Header().Get("Location"))

	// Check cookie
	cookies := w4.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "webdav_session" {
			found = true
			break
		}
	}
	assert.True(t, found, "Session cookie should be set")
}
