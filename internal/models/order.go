package models

import "time"

// OrderStatus представляет статус обработки заказа
type OrderStatus string

const (
	OrderStatusNew        OrderStatus = "NEW"
	OrderStatusProcessing OrderStatus = "PROCESSING"
	OrderStatusInvalid    OrderStatus = "INVALID"
	OrderStatusProcessed  OrderStatus = "PROCESSED"
)

// Order представляет заказ лояльности в системе
type Order struct {
	Number     string      `json:"number"`
	Status     OrderStatus `json:"status"`
	Accrual    *float64    `json:"accrual,omitempty"`
	UploadedAt time.Time   `json:"uploaded_at"`
}
