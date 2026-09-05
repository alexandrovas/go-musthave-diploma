package middleware

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const tokenExpiration = 24 * time.Hour

// generateJWT создаёт новый JWT-токен для заданного userID
func generateJWT(userID int64, secret string) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   fmt.Sprintf("%d", userID),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(tokenExpiration)),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// validateJWT проверяет токен и возвращает userID
func validateJWT(tokenString string, secret string) (int64, error) {
	token, err := jwt.ParseWithClaims(tokenString, &jwt.RegisteredClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return 0, err
	}

	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok || !token.Valid {
		return 0, fmt.Errorf("invalid token")
	}

	var userID int64
	_, err = fmt.Sscanf(claims.Subject, "%d", &userID)
	if err != nil {
		return 0, fmt.Errorf("invalid subject: %w", err)
	}

	return userID, nil
}

// BuildToken создаёт JWT-токен для заданного userID и подписывает его секретом
func BuildToken(userID int64, secret string) (string, error) {
	return generateJWT(userID, secret)
}
