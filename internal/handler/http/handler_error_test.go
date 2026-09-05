package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alexandrovas/go-musthave-diploma/internal/models"
	"github.com/alexandrovas/go-musthave-diploma/internal/service"
)

func TestRegisterUser_EmptyLoginOrPassword(t *testing.T) {
	h := NewHandler(&mockService{}, testHandlerLogger, "test-secret")

	body, _ := json.Marshal(models.LoginRequest{Login: "", Password: "pass"})
	req := httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.RegisterUser(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	body, _ = json.Marshal(models.LoginRequest{Login: "user", Password: ""})
	req = httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.RegisterUser(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRegisterUser_InternalError(t *testing.T) {
	svc := &mockService{
		registerUserFn: func(_ context.Context, _, _ string) (int64, error) {
			return 0, errors.New("db error")
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	body, _ := json.Marshal(models.LoginRequest{Login: "user", Password: "pass"})
	req := httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.RegisterUser(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestLoginUser_EmptyLoginOrPassword(t *testing.T) {
	h := NewHandler(&mockService{}, testHandlerLogger, "test-secret")

	body, _ := json.Marshal(models.LoginRequest{Login: "", Password: "pass"})
	req := httptest.NewRequest(http.MethodPost, "/api/user/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.LoginUser(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestLoginUser_InternalError(t *testing.T) {
	svc := &mockService{
		loginUserFn: func(_ context.Context, _, _ string) (int64, error) {
			return 0, errors.New("db error")
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	body, _ := json.Marshal(models.LoginRequest{Login: "user", Password: "pass"})
	req := httptest.NewRequest(http.MethodPost, "/api/user/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.LoginUser(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestUploadOrder_EmptyBody(t *testing.T) {
	h := NewHandler(&mockService{}, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/user/orders", bytes.NewReader([]byte("")))
	req.Header.Set("Content-Type", "text/plain")
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	h.UploadOrder(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadOrder_InternalError(t *testing.T) {
	svc := &mockService{
		uploadOrderFn: func(_ context.Context, _ int64, _ string) (service.OrderUploadStatus, error) {
			return 0, errors.New("db error")
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/user/orders", bytes.NewReader([]byte("9278923470")))
	req.Header.Set("Content-Type", "text/plain")
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	h.UploadOrder(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestUploadOrder_OrderAlreadyExists(t *testing.T) {
	svc := &mockService{
		uploadOrderFn: func(_ context.Context, _ int64, _ string) (service.OrderUploadStatus, error) {
			return 0, models.ErrOrderAlreadyExists
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/user/orders", bytes.NewReader([]byte("9278923470")))
	req.Header.Set("Content-Type", "text/plain")
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	h.UploadOrder(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code)
}

func TestGetOrders_InternalError(t *testing.T) {
	svc := &mockService{
		getOrdersFn: func(_ context.Context, _ int64) ([]models.Order, error) {
			return nil, errors.New("db error")
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/user/orders", nil)
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	h.GetOrders(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestGetBalance_InternalError(t *testing.T) {
	svc := &mockService{
		getBalanceFn: func(_ context.Context, _ int64) (*models.Balance, error) {
			return nil, errors.New("db error")
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/user/balance", nil)
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	h.GetBalance(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestWithdrawBalance_InvalidBody(t *testing.T) {
	h := NewHandler(&mockService{}, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", bytes.NewReader([]byte("bad json")))
	req.Header.Set("Content-Type", "application/json")
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	h.WithdrawBalance(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestWithdrawBalance_EmptyOrderOrZeroSum(t *testing.T) {
	h := NewHandler(&mockService{}, testHandlerLogger, "test-secret")

	// Empty order
	body, _ := json.Marshal(models.WithdrawRequest{Order: "", Sum: 100})
	req := httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	h.WithdrawBalance(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Zero sum
	body, _ = json.Marshal(models.WithdrawRequest{Order: "9278923470", Sum: 0})
	req = httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuth(1)(req)
	rec = httptest.NewRecorder()
	h.WithdrawBalance(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestWithdrawBalance_InternalError(t *testing.T) {
	svc := &mockService{
		withdrawBalanceFn: func(_ context.Context, _ int64, _ string, _ float64) error {
			return errors.New("db error")
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	body, _ := json.Marshal(models.WithdrawRequest{Order: "9278923470", Sum: 100})
	req := httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	h.WithdrawBalance(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestGetWithdrawals_InternalError(t *testing.T) {
	svc := &mockService{
		getWithdrawalsFn: func(_ context.Context, _ int64) ([]models.Withdrawal, error) {
			return nil, errors.New("db error")
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/user/withdrawals", nil)
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	h.GetWithdrawals(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestNewHandler(t *testing.T) {
	svc := &mockService{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewHandler(svc, logger, "secret")
	require.NotNil(t, h)
}

func TestGetOrders_NoAuth(t *testing.T) {
	h := NewHandler(&mockService{}, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/user/orders", nil)
	rec := httptest.NewRecorder()
	h.GetOrders(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetBalance_NoAuth(t *testing.T) {
	h := NewHandler(&mockService{}, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/user/balance", nil)
	rec := httptest.NewRecorder()
	h.GetBalance(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestWithdrawBalance_NoAuth(t *testing.T) {
	h := NewHandler(&mockService{}, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", nil)
	rec := httptest.NewRecorder()
	h.WithdrawBalance(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetWithdrawals_NoAuth(t *testing.T) {
	h := NewHandler(&mockService{}, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/user/withdrawals", nil)
	rec := httptest.NewRecorder()
	h.GetWithdrawals(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestWriteJSON(t *testing.T) {
	h := NewHandler(&mockService{}, testHandlerLogger, "test-secret")
	rec := httptest.NewRecorder()
	h.writeJSON(rec, models.ErrorResponse{Error: "test"}, http.StatusBadRequest)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}
