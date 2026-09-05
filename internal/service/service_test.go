package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/alexandrovas/go-musthave-diploma/internal/models"
)

var testLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

type mockRepository struct {
	createUserFn                 func(ctx context.Context, login, passwordHash string) (int64, error)
	getUserByLoginFn             func(ctx context.Context, login string) (*models.User, error)
	createOrderFn                func(ctx context.Context, userID int64, number string) (*models.Order, error)
	getOrderByNumberWithUserIDFn func(ctx context.Context, number string) (*models.Order, int64, error)
	getOrdersByUserIDFn          func(ctx context.Context, userID int64) ([]models.Order, error)
	updateOrderStatusFn          func(ctx context.Context, number string, status models.OrderStatus, accrual *float64) error
	getBalanceFn                 func(ctx context.Context, userID int64) (*models.Balance, error)
	createWithdrawalFn           func(ctx context.Context, userID int64, orderNumber string, sum float64) error
	getWithdrawalsByUserIDFn     func(ctx context.Context, userID int64) ([]models.Withdrawal, error)
	getOrdersForProcessingFn     func(ctx context.Context, limit int) ([]models.Order, error)

	getOrderByNumberWithUserIDCalls int
}

func (m *mockRepository) CreateUser(ctx context.Context, login, passwordHash string) (int64, error) {
	return m.createUserFn(ctx, login, passwordHash)
}
func (m *mockRepository) GetUserByLogin(ctx context.Context, login string) (*models.User, error) {
	return m.getUserByLoginFn(ctx, login)
}
func (m *mockRepository) CreateOrder(ctx context.Context, userID int64, number string) (*models.Order, error) {
	return m.createOrderFn(ctx, userID, number)
}
func (m *mockRepository) GetOrderByNumberWithUserID(ctx context.Context, number string) (*models.Order, int64, error) {
	m.getOrderByNumberWithUserIDCalls++
	return m.getOrderByNumberWithUserIDFn(ctx, number)
}
func (m *mockRepository) GetOrdersByUserID(ctx context.Context, userID int64) ([]models.Order, error) {
	return m.getOrdersByUserIDFn(ctx, userID)
}
func (m *mockRepository) UpdateOrderStatus(ctx context.Context, number string, status models.OrderStatus, accrual *float64) error {
	return m.updateOrderStatusFn(ctx, number, status, accrual)
}
func (m *mockRepository) GetBalance(ctx context.Context, userID int64) (*models.Balance, error) {
	return m.getBalanceFn(ctx, userID)
}
func (m *mockRepository) CreateWithdrawal(ctx context.Context, userID int64, orderNumber string, sum float64) error {
	return m.createWithdrawalFn(ctx, userID, orderNumber, sum)
}
func (m *mockRepository) GetWithdrawalsByUserID(ctx context.Context, userID int64) ([]models.Withdrawal, error) {
	return m.getWithdrawalsByUserIDFn(ctx, userID)
}
func (m *mockRepository) GetOrdersForProcessing(ctx context.Context, limit int) ([]models.Order, error) {
	return m.getOrdersForProcessingFn(ctx, limit)
}

func TestRegisterUser_Success(t *testing.T) {
	var gotLogin, gotHash string
	repo := &mockRepository{
		createUserFn: func(_ context.Context, login, passwordHash string) (int64, error) {
			gotLogin, gotHash = login, passwordHash
			return 42, nil
		},
	}
	svc := NewGophermartService(repo, testLogger)

	id, err := svc.RegisterUser(context.Background(), "user", "password")
	require.NoError(t, err)
	require.Equal(t, int64(42), id)
	require.Equal(t, "user", gotLogin)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(gotHash), []byte("password")))
}

func TestRegisterUser_EmptyCredentials(t *testing.T) {
	svc := NewGophermartService(&mockRepository{}, testLogger)

	_, err := svc.RegisterUser(context.Background(), "", "password")
	require.Error(t, err)

	_, err = svc.RegisterUser(context.Background(), "user", "")
	require.Error(t, err)
}

func TestRegisterUser_LoginTaken(t *testing.T) {
	repo := &mockRepository{
		createUserFn: func(_ context.Context, _, _ string) (int64, error) {
			return 0, models.ErrLoginTaken
		},
	}
	svc := NewGophermartService(repo, testLogger)

	_, err := svc.RegisterUser(context.Background(), "user", "password")
	require.ErrorIs(t, err, models.ErrLoginTaken)
}

func TestLoginUser_Success(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	require.NoError(t, err)

	repo := &mockRepository{
		getUserByLoginFn: func(_ context.Context, login string) (*models.User, error) {
			return &models.User{ID: 7, Login: login, Password: string(hash)}, nil
		},
	}
	svc := NewGophermartService(repo, testLogger)

	id, err := svc.LoginUser(context.Background(), "user", "password")
	require.NoError(t, err)
	require.Equal(t, int64(7), id)
}

func TestLoginUser_UserNotFound(t *testing.T) {
	repo := &mockRepository{
		getUserByLoginFn: func(_ context.Context, _ string) (*models.User, error) {
			return nil, models.ErrUserNotFound
		},
	}
	svc := NewGophermartService(repo, testLogger)

	_, err := svc.LoginUser(context.Background(), "user", "password")
	require.ErrorIs(t, err, models.ErrInvalidPassword)
}

func TestLoginUser_WrongPassword(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("correct"), bcrypt.DefaultCost)
	require.NoError(t, err)

	repo := &mockRepository{
		getUserByLoginFn: func(_ context.Context, login string) (*models.User, error) {
			return &models.User{ID: 7, Login: login, Password: string(hash)}, nil
		},
	}
	svc := NewGophermartService(repo, testLogger)

	_, err = svc.LoginUser(context.Background(), "user", "wrong")
	require.ErrorIs(t, err, models.ErrInvalidPassword)
}

func TestLoginUser_RepositoryError(t *testing.T) {
	repoErr := errors.New("db unavailable")
	repo := &mockRepository{
		getUserByLoginFn: func(_ context.Context, _ string) (*models.User, error) {
			return nil, repoErr
		},
	}
	svc := NewGophermartService(repo, testLogger)

	_, err := svc.LoginUser(context.Background(), "user", "password")
	require.ErrorIs(t, err, repoErr)
}

func TestUploadOrder_NewOrderAccepted(t *testing.T) {
	repo := &mockRepository{
		getOrderByNumberWithUserIDFn: func(_ context.Context, _ string) (*models.Order, int64, error) {
			return nil, 0, models.ErrOrderNotFound
		},
		createOrderFn: func(_ context.Context, _ int64, _ string) (*models.Order, error) {
			return &models.Order{}, nil
		},
	}
	svc := NewGophermartService(repo, testLogger)

	status, err := svc.UploadOrder(context.Background(), 1, "12345")
	require.NoError(t, err)
	require.Equal(t, UploadAccepted, status)
}

func TestUploadOrder_AlreadyByUser(t *testing.T) {
	repo := &mockRepository{
		getOrderByNumberWithUserIDFn: func(_ context.Context, _ string) (*models.Order, int64, error) {
			return &models.Order{Number: "12345"}, 1, nil
		},
	}
	svc := NewGophermartService(repo, testLogger)

	status, err := svc.UploadOrder(context.Background(), 1, "12345")
	require.NoError(t, err)
	require.Equal(t, UploadAlreadyByUser, status)
}

func TestUploadOrder_AlreadyByOther(t *testing.T) {
	repo := &mockRepository{
		getOrderByNumberWithUserIDFn: func(_ context.Context, _ string) (*models.Order, int64, error) {
			return &models.Order{Number: "12345"}, 2, nil
		},
	}
	svc := NewGophermartService(repo, testLogger)

	status, err := svc.UploadOrder(context.Background(), 1, "12345")
	require.NoError(t, err)
	require.Equal(t, UploadAlreadyByOther, status)
}

func TestUploadOrder_LookupError(t *testing.T) {
	repoErr := errors.New("db unavailable")
	repo := &mockRepository{
		getOrderByNumberWithUserIDFn: func(_ context.Context, _ string) (*models.Order, int64, error) {
			return nil, 0, repoErr
		},
	}
	svc := NewGophermartService(repo, testLogger)

	_, err := svc.UploadOrder(context.Background(), 1, "12345")
	require.ErrorIs(t, err, repoErr)
}

func TestUploadOrder_CreateError(t *testing.T) {
	repoErr := errors.New("db unavailable")
	repo := &mockRepository{
		getOrderByNumberWithUserIDFn: func(_ context.Context, _ string) (*models.Order, int64, error) {
			return nil, 0, models.ErrOrderNotFound
		},
		createOrderFn: func(_ context.Context, _ int64, _ string) (*models.Order, error) {
			return nil, repoErr
		},
	}
	svc := NewGophermartService(repo, testLogger)

	_, err := svc.UploadOrder(context.Background(), 1, "12345")
	require.ErrorIs(t, err, repoErr)
}

// TestUploadOrder_RaceCreatedByUser покрывает race condition:
// между проверкой отсутствия заказа и вставкой заказ был создан этим же пользователем.
func TestUploadOrder_RaceCreatedByUser(t *testing.T) {
	// первая проверка — заказа нет; вторая (после конфликта вставки) — заказ есть, владелец тот же пользователь
	calls := 0
	repo := &mockRepository{
		getOrderByNumberWithUserIDFn: func(_ context.Context, _ string) (*models.Order, int64, error) {
			calls++
			if calls == 1 {
				return nil, 0, models.ErrOrderNotFound
			}
			return &models.Order{Number: "12345"}, 1, nil
		},
		createOrderFn: func(_ context.Context, _ int64, _ string) (*models.Order, error) {
			return nil, models.ErrOrderAlreadyExists
		},
	}

	svc := NewGophermartService(repo, testLogger)

	status, err := svc.UploadOrder(context.Background(), 1, "12345")
	require.NoError(t, err)
	require.Equal(t, UploadAlreadyByUser, status)
	require.Equal(t, 2, repo.getOrderByNumberWithUserIDCalls)
}

// TestUploadOrder_RaceCreatedByOther покрывает race condition:
// заказ создан параллельно другим пользователем.
func TestUploadOrder_RaceCreatedByOther(t *testing.T) {
	repo := &mockRepository{
		createOrderFn: func(_ context.Context, _ int64, _ string) (*models.Order, error) {
			return nil, models.ErrOrderAlreadyExists
		},
	}
	calls := 0
	repo.getOrderByNumberWithUserIDFn = func(_ context.Context, _ string) (*models.Order, int64, error) {
		calls++
		if calls == 1 {
			return nil, 0, models.ErrOrderNotFound
		}
		return &models.Order{Number: "12345"}, 2, nil
	}

	svc := NewGophermartService(repo, testLogger)

	status, err := svc.UploadOrder(context.Background(), 1, "12345")
	require.NoError(t, err)
	require.Equal(t, UploadAlreadyByOther, status)
}

func TestUploadOrder_RaceRecheckError(t *testing.T) {
	repoErr := errors.New("db unavailable")
	calls := 0
	repo := &mockRepository{
		getOrderByNumberWithUserIDFn: func(_ context.Context, _ string) (*models.Order, int64, error) {
			calls++
			if calls == 1 {
				return nil, 0, models.ErrOrderNotFound
			}
			return nil, 0, repoErr
		},
		createOrderFn: func(_ context.Context, _ int64, _ string) (*models.Order, error) {
			return nil, models.ErrOrderAlreadyExists
		},
	}
	svc := NewGophermartService(repo, testLogger)

	_, err := svc.UploadOrder(context.Background(), 1, "12345")
	require.ErrorIs(t, err, repoErr)
}

func TestGetOrders(t *testing.T) {
	want := []models.Order{{Number: "123", Status: models.OrderStatusNew}}
	repo := &mockRepository{
		getOrdersByUserIDFn: func(_ context.Context, userID int64) ([]models.Order, error) {
			require.Equal(t, int64(1), userID)
			return want, nil
		},
	}
	svc := NewGophermartService(repo, testLogger)

	got, err := svc.GetOrders(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestGetBalance(t *testing.T) {
	want := &models.Balance{Current: 100, Withdrawn: 50}
	repo := &mockRepository{
		getBalanceFn: func(_ context.Context, userID int64) (*models.Balance, error) {
			require.Equal(t, int64(1), userID)
			return want, nil
		},
	}
	svc := NewGophermartService(repo, testLogger)

	got, err := svc.GetBalance(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestWithdrawBalance_Success(t *testing.T) {
	var gotUserID int64
	var gotOrder string
	var gotSum float64
	repo := &mockRepository{
		createWithdrawalFn: func(_ context.Context, userID int64, orderNumber string, sum float64) error {
			gotUserID, gotOrder, gotSum = userID, orderNumber, sum
			return nil
		},
	}
	svc := NewGophermartService(repo, testLogger)

	err := svc.WithdrawBalance(context.Background(), 1, "12345", 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), gotUserID)
	require.Equal(t, "12345", gotOrder)
	require.InDelta(t, 100, gotSum, 0.001)
}

func TestWithdrawBalance_InsufficientFunds(t *testing.T) {
	repo := &mockRepository{
		createWithdrawalFn: func(_ context.Context, _ int64, _ string, _ float64) error {
			return models.ErrInsufficientFunds
		},
	}
	svc := NewGophermartService(repo, testLogger)

	err := svc.WithdrawBalance(context.Background(), 1, "12345", 100)
	require.ErrorIs(t, err, models.ErrInsufficientFunds)
}

func TestGetWithdrawals(t *testing.T) {
	want := []models.Withdrawal{{Order: "123", Sum: 500}}
	repo := &mockRepository{
		getWithdrawalsByUserIDFn: func(_ context.Context, userID int64) ([]models.Withdrawal, error) {
			require.Equal(t, int64(1), userID)
			return want, nil
		},
	}
	svc := NewGophermartService(repo, testLogger)

	got, err := svc.GetWithdrawals(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, want, got)
}
