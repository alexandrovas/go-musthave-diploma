package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alexandrovas/go-musthave-diploma/internal/middleware"
	"github.com/alexandrovas/go-musthave-diploma/internal/models"
	"github.com/alexandrovas/go-musthave-diploma/internal/service"
)

var testHandlerLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

type mockService struct {
	registerUserFn    func(ctx context.Context, login, password string) (int64, error)
	loginUserFn       func(ctx context.Context, login, password string) (int64, error)
	uploadOrderFn     func(ctx context.Context, userID int64, orderNumber string) (service.OrderUploadStatus, error)
	getOrdersFn       func(ctx context.Context, userID int64) ([]models.Order, error)
	getBalanceFn      func(ctx context.Context, userID int64) (*models.Balance, error)
	withdrawBalanceFn func(ctx context.Context, userID int64, orderNumber string, sum float64) error
	getWithdrawalsFn  func(ctx context.Context, userID int64) ([]models.Withdrawal, error)
}

func (m *mockService) RegisterUser(ctx context.Context, login, password string) (int64, error) {
	return m.registerUserFn(ctx, login, password)
}
func (m *mockService) LoginUser(ctx context.Context, login, password string) (int64, error) {
	return m.loginUserFn(ctx, login, password)
}
func (m *mockService) UploadOrder(ctx context.Context, userID int64, orderNumber string) (service.OrderUploadStatus, error) {
	return m.uploadOrderFn(ctx, userID, orderNumber)
}
func (m *mockService) GetOrders(ctx context.Context, userID int64) ([]models.Order, error) {
	return m.getOrdersFn(ctx, userID)
}
func (m *mockService) GetBalance(ctx context.Context, userID int64) (*models.Balance, error) {
	return m.getBalanceFn(ctx, userID)
}
func (m *mockService) WithdrawBalance(ctx context.Context, userID int64, orderNumber string, sum float64) error {
	return m.withdrawBalanceFn(ctx, userID, orderNumber, sum)
}
func (m *mockService) GetWithdrawals(ctx context.Context, userID int64) ([]models.Withdrawal, error) {
	return m.getWithdrawalsFn(ctx, userID)
}

const testJWTSecret = "test-secret"

func withAuth(userID int64) func(r *http.Request) {
	return func(r *http.Request) {
		token, err := middleware.BuildToken(userID, testJWTSecret)
		if err != nil {
			panic(err)
		}
		r.AddCookie(&http.Cookie{Name: "token", Value: token})
	}
}

// serveAuthed прогоняет запрос через настоящий middleware.Auth перед вызовом хендлера,
// чтобы тесты не зависели от способа, которым middleware помещает userID в контекст
func serveAuthed(next http.HandlerFunc, w http.ResponseWriter, r *http.Request) {
	middleware.Auth(testJWTSecret)(next).ServeHTTP(w, r)
}

func TestRegisterUser_Success(t *testing.T) {
	svc := &mockService{
		registerUserFn: func(_ context.Context, _, _ string) (int64, error) {
			return 1, nil
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	body, _ := json.Marshal(models.LoginRequest{Login: "user", Password: "pass"})
	req := httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.RegisterUser(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestRegisterUser_InvalidBody(t *testing.T) {
	h := NewHandler(&mockService{}, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.RegisterUser(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRegisterUser_LoginTaken(t *testing.T) {
	svc := &mockService{
		registerUserFn: func(_ context.Context, _, _ string) (int64, error) {
			return 0, models.ErrLoginTaken
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	body, _ := json.Marshal(models.LoginRequest{Login: "user", Password: "pass"})
	req := httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.RegisterUser(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code)
}

func TestLoginUser_Success(t *testing.T) {
	svc := &mockService{
		loginUserFn: func(_ context.Context, _, _ string) (int64, error) {
			return 1, nil
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	body, _ := json.Marshal(models.LoginRequest{Login: "user", Password: "pass"})
	req := httptest.NewRequest(http.MethodPost, "/api/user/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.LoginUser(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestLoginUser_InvalidCredentials(t *testing.T) {
	svc := &mockService{
		loginUserFn: func(_ context.Context, _, _ string) (int64, error) {
			return 0, models.ErrInvalidPassword
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	body, _ := json.Marshal(models.LoginRequest{Login: "user", Password: "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/api/user/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.LoginUser(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestLoginUser_InvalidBody(t *testing.T) {
	h := NewHandler(&mockService{}, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/user/login", bytes.NewReader([]byte("bad")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.LoginUser(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadOrder_Accepted(t *testing.T) {
	svc := &mockService{
		uploadOrderFn: func(_ context.Context, _ int64, _ string) (service.OrderUploadStatus, error) {
			return service.UploadAccepted, nil
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/user/orders", bytes.NewReader([]byte("9278923470")))
	req.Header.Set("Content-Type", "text/plain")
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	serveAuthed(h.UploadOrder, rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code)
}

func TestUploadOrder_AlreadyByUser(t *testing.T) {
	svc := &mockService{
		uploadOrderFn: func(_ context.Context, _ int64, _ string) (service.OrderUploadStatus, error) {
			return service.UploadAlreadyByUser, nil
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/user/orders", bytes.NewReader([]byte("9278923470")))
	req.Header.Set("Content-Type", "text/plain")
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	serveAuthed(h.UploadOrder, rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestUploadOrder_AlreadyByOther(t *testing.T) {
	svc := &mockService{
		uploadOrderFn: func(_ context.Context, _ int64, _ string) (service.OrderUploadStatus, error) {
			return service.UploadAlreadyByOther, nil
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/user/orders", bytes.NewReader([]byte("9278923470")))
	req.Header.Set("Content-Type", "text/plain")
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	serveAuthed(h.UploadOrder, rec, req)
	require.Equal(t, http.StatusConflict, rec.Code)
}

func TestUploadOrder_InvalidLuhn(t *testing.T) {
	h := NewHandler(&mockService{}, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/user/orders", bytes.NewReader([]byte("12345678901")))
	req.Header.Set("Content-Type", "text/plain")
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	serveAuthed(h.UploadOrder, rec, req)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestUploadOrder_NoAuth(t *testing.T) {
	h := NewHandler(&mockService{}, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/user/orders", bytes.NewReader([]byte("9278923470")))
	rec := httptest.NewRecorder()
	h.UploadOrder(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetOrders_Success(t *testing.T) {
	svc := &mockService{
		getOrdersFn: func(_ context.Context, _ int64) ([]models.Order, error) {
			return []models.Order{{Number: "123", Status: models.OrderStatusNew}}, nil
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/user/orders", nil)
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	serveAuthed(h.GetOrders, rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestGetOrders_NoContent(t *testing.T) {
	svc := &mockService{
		getOrdersFn: func(_ context.Context, _ int64) ([]models.Order, error) {
			return nil, nil
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/user/orders", nil)
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	serveAuthed(h.GetOrders, rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)
}

func TestGetBalance_Success(t *testing.T) {
	svc := &mockService{
		getBalanceFn: func(_ context.Context, _ int64) (*models.Balance, error) {
			return &models.Balance{Current: 500.5, Withdrawn: 42}, nil
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/user/balance", nil)
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	serveAuthed(h.GetBalance, rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var bal models.Balance
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&bal))
	require.InDelta(t, 500.5, bal.Current, 0.01)
	require.InDelta(t, 42, bal.Withdrawn, 0.01)
}

func TestWithdrawBalance_Success(t *testing.T) {
	svc := &mockService{
		withdrawBalanceFn: func(_ context.Context, _ int64, _ string, _ float64) error {
			return nil
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	body, _ := json.Marshal(models.WithdrawRequest{Order: "9278923470", Sum: 100})
	req := httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	serveAuthed(h.WithdrawBalance, rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestWithdrawBalance_InsufficientFunds(t *testing.T) {
	svc := &mockService{
		withdrawBalanceFn: func(_ context.Context, _ int64, _ string, _ float64) error {
			return models.ErrInsufficientFunds
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	body, _ := json.Marshal(models.WithdrawRequest{Order: "9278923470", Sum: 9999})
	req := httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	serveAuthed(h.WithdrawBalance, rec, req)
	require.Equal(t, http.StatusPaymentRequired, rec.Code)
}

func TestWithdrawBalance_InvalidLuhn(t *testing.T) {
	h := NewHandler(&mockService{}, testHandlerLogger, "test-secret")
	body, _ := json.Marshal(models.WithdrawRequest{Order: "12345678901", Sum: 100})
	req := httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	serveAuthed(h.WithdrawBalance, rec, req)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestGetWithdrawals_Success(t *testing.T) {
	svc := &mockService{
		getWithdrawalsFn: func(_ context.Context, _ int64) ([]models.Withdrawal, error) {
			return []models.Withdrawal{{Order: "123", Sum: 500}}, nil
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/user/withdrawals", nil)
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	serveAuthed(h.GetWithdrawals, rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestGetWithdrawals_NoContent(t *testing.T) {
	svc := &mockService{
		getWithdrawalsFn: func(_ context.Context, _ int64) ([]models.Withdrawal, error) {
			return nil, nil
		},
	}
	h := NewHandler(svc, testHandlerLogger, "test-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/user/withdrawals", nil)
	withAuth(1)(req)
	rec := httptest.NewRecorder()
	serveAuthed(h.GetWithdrawals, rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)
}
