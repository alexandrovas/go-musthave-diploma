package models

import "errors"

// ErrorResponse представляет тело JSON-ответа с ошибкой
type ErrorResponse struct {
	Error string `json:"error"`
}

// Sentinel-ошибки, используемые в service и repository
var (
	ErrLoginTaken       = errors.New("login already taken")
	ErrUserNotFound     = errors.New("user not found")
	ErrInvalidPassword  = errors.New("invalid password")
	ErrOrderNotFound    = errors.New("order not found")
	ErrOrderAlreadyExists = errors.New("order already exists")
	ErrInsufficientFunds = errors.New("insufficient funds")
)
