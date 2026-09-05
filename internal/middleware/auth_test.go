package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserIDFromContext_Missing(t *testing.T) {
	_, ok := UserIDFromContext(httptest.NewRequest(http.MethodGet, "/", nil).Context())
	require.False(t, ok)
}

func TestSetAuthCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	SetAuthCookie(rec, "test-token")
	cookie := rec.Result().Cookies()
	require.Len(t, cookie, 1)
	require.Equal(t, "token", cookie[0].Name)
	require.Equal(t, "test-token", cookie[0].Value)
	require.True(t, cookie[0].HttpOnly)
	require.Equal(t, "/", cookie[0].Path)
}

func TestAuth_MissingCookie(t *testing.T) {
	handler := Auth("secret")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuth_InvalidToken(t *testing.T) {
	handler := Auth("secret")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: "invalid-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuth_ValidToken(t *testing.T) {
	secret := "test-secret"
	token, err := BuildToken(42, secret)
	require.NoError(t, err)

	var called bool
	handler := Auth(secret)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		called = true
		userID, ok := UserIDFromContext(r.Context())
		require.True(t, ok)
		require.Equal(t, int64(42), userID)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.True(t, called)
}
