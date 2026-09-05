package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/alexandrovas/go-musthave-diploma/internal/luhn"
	"github.com/alexandrovas/go-musthave-diploma/internal/middleware"
	"github.com/alexandrovas/go-musthave-diploma/internal/models"
	"github.com/alexandrovas/go-musthave-diploma/internal/service"
)

// GophermartService — интерфейс сервиса, определяемый на стороне потребителя (handler)
type GophermartService interface {
	RegisterUser(ctx context.Context, login, password string) (userID int64, err error)
	LoginUser(ctx context.Context, login, password string) (userID int64, err error)
	UploadOrder(ctx context.Context, userID int64, orderNumber string) (status service.OrderUploadStatus, err error)
	GetOrders(ctx context.Context, userID int64) ([]models.Order, error)
	GetBalance(ctx context.Context, userID int64) (*models.Balance, error)
	WithdrawBalance(ctx context.Context, userID int64, orderNumber string, sum float64) error
	GetWithdrawals(ctx context.Context, userID int64) ([]models.Withdrawal, error)
}

// Handler — структура HTTP-хендлеров сервиса gophermart
type Handler struct {
	service   GophermartService
	logger    *slog.Logger
	jwtSecret string
}

// NewHandler создаёт новый экземпляр Handler
func NewHandler(s GophermartService, logger *slog.Logger, jwtSecret string) *Handler {
	return &Handler{
		service:   s,
		logger:    logger,
		jwtSecret: jwtSecret,
	}
}

// writeJSON записывает JSON-ответ с заданным статус-кодом
func (h *Handler) writeJSON(w http.ResponseWriter, resp any, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Debug("error encoding JSON response", "error", err)
	}
}

// RegisterUser — POST /api/user/register
func (h *Handler) RegisterUser(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, models.ErrorResponse{Error: errInvalidRequestBody.Error()}, http.StatusBadRequest)
		return
	}

	if req.Login == "" || req.Password == "" {
		h.writeJSON(w, models.ErrorResponse{Error: errInvalidRequestBody.Error()}, http.StatusBadRequest)
		return
	}

	userID, err := h.service.RegisterUser(r.Context(), req.Login, req.Password)
	if err != nil {
		if errors.Is(err, models.ErrLoginTaken) {
			h.writeJSON(w, models.ErrorResponse{Error: errLoginTaken.Error()}, http.StatusConflict)
			return
		}
		h.logger.Error("failed to register user", "error", err)
		h.writeJSON(w, models.ErrorResponse{Error: errInternal.Error()}, http.StatusInternalServerError)
		return
	}

	// Генерация JWT и установка cookie
	token, err := middleware.BuildToken(userID, h.jwtSecret)
	if err != nil {
		h.logger.Error("failed to build token", "error", err)
		h.writeJSON(w, models.ErrorResponse{Error: errInternal.Error()}, http.StatusInternalServerError)
		return
	}
	middleware.SetAuthCookie(w, r, token)

	w.WriteHeader(http.StatusOK)
}

// LoginUser — POST /api/user/login
func (h *Handler) LoginUser(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, models.ErrorResponse{Error: errInvalidRequestBody.Error()}, http.StatusBadRequest)
		return
	}

	if req.Login == "" || req.Password == "" {
		h.writeJSON(w, models.ErrorResponse{Error: errInvalidRequestBody.Error()}, http.StatusBadRequest)
		return
	}

	userID, err := h.service.LoginUser(r.Context(), req.Login, req.Password)
	if err != nil {
		if errors.Is(err, models.ErrInvalidPassword) {
			h.writeJSON(w, models.ErrorResponse{Error: errInvalidCredentials.Error()}, http.StatusUnauthorized)
			return
		}
		h.logger.Error("failed to authenticate user", "error", err)
		h.writeJSON(w, models.ErrorResponse{Error: errInternal.Error()}, http.StatusInternalServerError)
		return
	}

	// Генерация JWT и установка cookie
	token, err := middleware.BuildToken(userID, h.jwtSecret)
	if err != nil {
		h.logger.Error("failed to build token", "error", err)
		h.writeJSON(w, models.ErrorResponse{Error: errInternal.Error()}, http.StatusInternalServerError)
		return
	}
	middleware.SetAuthCookie(w, r, token)

	w.WriteHeader(http.StatusOK)
}

// UploadOrder — POST /api/user/orders
func (h *Handler) UploadOrder(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Чтение тела как text/plain — номер заказа
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeJSON(w, models.ErrorResponse{Error: errInvalidRequestBody.Error()}, http.StatusBadRequest)
		return
	}

	orderNumber := string(body)
	if orderNumber == "" {
		h.writeJSON(w, models.ErrorResponse{Error: errInvalidRequestBody.Error()}, http.StatusBadRequest)
		return
	}

	// Валидация номера заказа алгоритмом Луна
	if !luhn.IsValid(orderNumber) {
		h.writeJSON(w, models.ErrorResponse{Error: errInvalidOrderNumber.Error()}, http.StatusUnprocessableEntity)
		return
	}

	status, err := h.service.UploadOrder(r.Context(), userID, orderNumber)
	if err != nil {
		if errors.Is(err, models.ErrOrderAlreadyExists) {
			h.writeJSON(w, models.ErrorResponse{Error: errOrderAlreadyUploaded.Error()}, http.StatusConflict)
			return
		}
		h.logger.Error("failed to upload order", "error", err)
		h.writeJSON(w, models.ErrorResponse{Error: errInternal.Error()}, http.StatusInternalServerError)
		return
	}

	switch status {
	case service.UploadAlreadyByUser:
		w.WriteHeader(http.StatusOK)
	case service.UploadAccepted:
		w.WriteHeader(http.StatusAccepted)
	case service.UploadAlreadyByOther:
		h.writeJSON(w, models.ErrorResponse{Error: errOrderAlreadyUploaded.Error()}, http.StatusConflict)
	default:
		w.WriteHeader(http.StatusAccepted)
	}
}

// GetOrders — GET /api/user/orders
func (h *Handler) GetOrders(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	orders, err := h.service.GetOrders(r.Context(), userID)
	if err != nil {
		h.logger.Error("failed to get orders", "error", err)
		h.writeJSON(w, models.ErrorResponse{Error: errInternal.Error()}, http.StatusInternalServerError)
		return
	}

	if len(orders) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	h.writeJSON(w, orders, http.StatusOK)
}

// GetBalance — GET /api/user/balance
func (h *Handler) GetBalance(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	balance, err := h.service.GetBalance(r.Context(), userID)
	if err != nil {
		h.logger.Error("failed to get balance", "error", err)
		h.writeJSON(w, models.ErrorResponse{Error: errInternal.Error()}, http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, balance, http.StatusOK)
}

// WithdrawBalance — POST /api/user/balance/withdraw
func (h *Handler) WithdrawBalance(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req models.WithdrawRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, models.ErrorResponse{Error: errInvalidRequestBody.Error()}, http.StatusBadRequest)
		return
	}

	if req.Order == "" || req.Sum <= 0 {
		h.writeJSON(w, models.ErrorResponse{Error: errInvalidRequestBody.Error()}, http.StatusBadRequest)
		return
	}

	// Валидация номера заказа алгоритмом Луна
	if !luhn.IsValid(req.Order) {
		h.writeJSON(w, models.ErrorResponse{Error: errInvalidOrderNumber.Error()}, http.StatusUnprocessableEntity)
		return
	}

	err := h.service.WithdrawBalance(r.Context(), userID, req.Order, req.Sum)
	if err != nil {
		if errors.Is(err, models.ErrInsufficientFunds) {
			h.writeJSON(w, models.ErrorResponse{Error: errInsufficientFunds.Error()}, http.StatusPaymentRequired)
			return
		}
		h.logger.Error("failed to withdraw balance", "error", err)
		h.writeJSON(w, models.ErrorResponse{Error: errInternal.Error()}, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GetWithdrawals — GET /api/user/withdrawals
func (h *Handler) GetWithdrawals(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	withdrawals, err := h.service.GetWithdrawals(r.Context(), userID)
	if err != nil {
		h.logger.Error("failed to get withdrawals", "error", err)
		h.writeJSON(w, models.ErrorResponse{Error: errInternal.Error()}, http.StatusInternalServerError)
		return
	}

	if len(withdrawals) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	h.writeJSON(w, withdrawals, http.StatusOK)
}
