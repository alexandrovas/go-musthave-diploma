package middleware

import (
	"context"
	"net/http"
)

type contextKey string

const contextKeyUserID contextKey = "userID"

// Auth возвращает middleware, проверяющее JWT-токен из cookie и извлекающее userID
func Auth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("token")
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			userID, err := validateJWT(cookie.Value, secret)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), contextKeyUserID, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFromContext извлекает userID из контекста запроса
func UserIDFromContext(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(contextKeyUserID).(int64)
	return id, ok
}

// SetAuthCookie устанавливает cookie с JWT-токеном в ответ.
// Secure выставляется только для запросов по HTTPS, чтобы не сломать
// работу сервиса при развёртывании по обычному HTTP
func SetAuthCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}
