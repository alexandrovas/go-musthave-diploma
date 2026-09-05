package models

import "time"

// Balance представляет текущий баланс баллов лояльности пользователя
type Balance struct {
	Current   float64 `json:"current"`
	Withdrawn float64 `json:"withdrawn"`
}

// Withdrawal представляет одно списание с накопительного счёта пользователя
type Withdrawal struct {
	Order       string    `json:"order"`
	Sum         float64   `json:"sum"`
	ProcessedAt time.Time `json:"processed_at"`
}

// WithdrawRequest представляет JSON-тело запроса на списание баллов
type WithdrawRequest struct {
	Order string  `json:"order"`
	Sum   float64 `json:"sum"`
}
