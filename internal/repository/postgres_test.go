package repository

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alexandrovas/go-musthave-diploma/internal/models"
)

var testLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// newTestStorage подключается к тестовой БД, заданной переменной окружения
// TEST_DATABASE_URI, накатывает миграции и очищает таблицы перед тестом.
// При отсутствии переменной окружения тест пропускается.
func newTestStorage(t *testing.T) *PostgresStorage {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URI")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URI is not set, skipping repository integration tests")
	}

	ctx := context.Background()
	s, err := NewPostgresStorage(ctx, testLogger, dsn)
	require.NoError(t, err)

	_, err = s.db.ExecContext(ctx, `TRUNCATE TABLE withdrawals, orders, users RESTART IDENTITY CASCADE`)
	require.NoError(t, err)

	t.Cleanup(s.Close)

	return s
}

func createTestUser(t *testing.T, s *PostgresStorage, login string) int64 {
	t.Helper()
	id, err := s.CreateUser(context.Background(), login, "password-hash")
	require.NoError(t, err)
	return id
}

func TestCreateUser_Success(t *testing.T) {
	s := newTestStorage(t)

	id, err := s.CreateUser(context.Background(), "alice", "hash")
	require.NoError(t, err)
	require.NotZero(t, id)
}

func TestCreateUser_DuplicateLogin(t *testing.T) {
	s := newTestStorage(t)
	createTestUser(t, s, "alice")

	_, err := s.CreateUser(context.Background(), "alice", "other-hash")
	require.ErrorIs(t, err, models.ErrLoginTaken)
}

func TestGetUserByLogin_Success(t *testing.T) {
	s := newTestStorage(t)
	id := createTestUser(t, s, "alice")

	u, err := s.GetUserByLogin(context.Background(), "alice")
	require.NoError(t, err)
	require.Equal(t, id, u.ID)
	require.Equal(t, "alice", u.Login)
	require.Equal(t, "password-hash", u.Password)
}

func TestGetUserByLogin_NotFound(t *testing.T) {
	s := newTestStorage(t)

	_, err := s.GetUserByLogin(context.Background(), "ghost")
	require.ErrorIs(t, err, models.ErrUserNotFound)
}

func TestCreateOrder_Success(t *testing.T) {
	s := newTestStorage(t)
	userID := createTestUser(t, s, "alice")

	o, err := s.CreateOrder(context.Background(), userID, "12345")
	require.NoError(t, err)
	require.Equal(t, "12345", o.Number)
	require.Equal(t, models.OrderStatusNew, o.Status)
	require.Nil(t, o.Accrual)
}

func TestCreateOrder_Duplicate(t *testing.T) {
	s := newTestStorage(t)
	userID := createTestUser(t, s, "alice")
	_, err := s.CreateOrder(context.Background(), userID, "12345")
	require.NoError(t, err)

	_, err = s.CreateOrder(context.Background(), userID, "12345")
	require.ErrorIs(t, err, models.ErrOrderAlreadyExists)
}

func TestGetOrderByNumberWithUserID_Success(t *testing.T) {
	s := newTestStorage(t)
	userID := createTestUser(t, s, "alice")
	_, err := s.CreateOrder(context.Background(), userID, "12345")
	require.NoError(t, err)

	o, ownerID, err := s.GetOrderByNumberWithUserID(context.Background(), "12345")
	require.NoError(t, err)
	require.Equal(t, userID, ownerID)
	require.Equal(t, "12345", o.Number)
}

func TestGetOrderByNumberWithUserID_NotFound(t *testing.T) {
	s := newTestStorage(t)

	_, _, err := s.GetOrderByNumberWithUserID(context.Background(), "does-not-exist")
	require.ErrorIs(t, err, models.ErrOrderNotFound)
}

func TestGetOrdersByUserID_OrderedNewestFirst(t *testing.T) {
	s := newTestStorage(t)
	userID := createTestUser(t, s, "alice")

	_, err := s.CreateOrder(context.Background(), userID, "111")
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)
	_, err = s.CreateOrder(context.Background(), userID, "222")
	require.NoError(t, err)

	orders, err := s.GetOrdersByUserID(context.Background(), userID)
	require.NoError(t, err)
	require.Len(t, orders, 2)
	require.Equal(t, "222", orders[0].Number)
	require.Equal(t, "111", orders[1].Number)
}

func TestGetOrdersByUserID_Empty(t *testing.T) {
	s := newTestStorage(t)
	userID := createTestUser(t, s, "alice")

	orders, err := s.GetOrdersByUserID(context.Background(), userID)
	require.NoError(t, err)
	require.Empty(t, orders)
}

func TestUpdateOrderStatus(t *testing.T) {
	s := newTestStorage(t)
	userID := createTestUser(t, s, "alice")
	_, err := s.CreateOrder(context.Background(), userID, "12345")
	require.NoError(t, err)

	accrual := 100.5
	err = s.UpdateOrderStatus(context.Background(), "12345", models.OrderStatusProcessed, &accrual)
	require.NoError(t, err)

	o, _, err := s.GetOrderByNumberWithUserID(context.Background(), "12345")
	require.NoError(t, err)
	require.Equal(t, models.OrderStatusProcessed, o.Status)
	require.NotNil(t, o.Accrual)
	require.InDelta(t, accrual, *o.Accrual, 0.001)
}

func TestGetBalance_NoActivity(t *testing.T) {
	s := newTestStorage(t)
	userID := createTestUser(t, s, "alice")

	b, err := s.GetBalance(context.Background(), userID)
	require.NoError(t, err)
	require.InDelta(t, 0, b.Current, 0.001)
	require.InDelta(t, 0, b.Withdrawn, 0.001)
}

// TestGetBalance_SubtractsWithdrawals — регрессионный тест на исправленный баг:
// current должен быть доступным балансом (начисления минус списания),
// а не полной суммой начислений.
func TestGetBalance_SubtractsWithdrawals(t *testing.T) {
	s := newTestStorage(t)
	userID := createTestUser(t, s, "alice")

	_, err := s.CreateOrder(context.Background(), userID, "12345")
	require.NoError(t, err)
	accrual := 500.0
	require.NoError(t, s.UpdateOrderStatus(context.Background(), "12345", models.OrderStatusProcessed, &accrual))

	require.NoError(t, s.CreateWithdrawal(context.Background(), userID, "999", 200))

	b, err := s.GetBalance(context.Background(), userID)
	require.NoError(t, err)
	require.InDelta(t, 300, b.Current, 0.001)
	require.InDelta(t, 200, b.Withdrawn, 0.001)
}

func TestGetBalance_IgnoresNonProcessedOrders(t *testing.T) {
	s := newTestStorage(t)
	userID := createTestUser(t, s, "alice")

	_, err := s.CreateOrder(context.Background(), userID, "12345")
	require.NoError(t, err)
	// заказ остаётся в статусе NEW — начисления быть не должно

	b, err := s.GetBalance(context.Background(), userID)
	require.NoError(t, err)
	require.InDelta(t, 0, b.Current, 0.001)
}

func TestCreateWithdrawal_Success(t *testing.T) {
	s := newTestStorage(t)
	userID := createTestUser(t, s, "alice")
	_, err := s.CreateOrder(context.Background(), userID, "12345")
	require.NoError(t, err)
	accrual := 500.0
	require.NoError(t, s.UpdateOrderStatus(context.Background(), "12345", models.OrderStatusProcessed, &accrual))

	err = s.CreateWithdrawal(context.Background(), userID, "999", 300)
	require.NoError(t, err)

	withdrawals, err := s.GetWithdrawalsByUserID(context.Background(), userID)
	require.NoError(t, err)
	require.Len(t, withdrawals, 1)
	require.Equal(t, "999", withdrawals[0].Order)
	require.InDelta(t, 300, withdrawals[0].Sum, 0.001)
}

func TestCreateWithdrawal_InsufficientFunds(t *testing.T) {
	s := newTestStorage(t)
	userID := createTestUser(t, s, "alice")

	err := s.CreateWithdrawal(context.Background(), userID, "999", 100)
	require.ErrorIs(t, err, models.ErrInsufficientFunds)
}

// TestCreateWithdrawal_ConcurrentSerialized проверяет, что advisory lock
// действительно сериализует параллельные списания одного пользователя,
// не допуская ухода баланса в минус.
func TestCreateWithdrawal_ConcurrentSerialized(t *testing.T) {
	s := newTestStorage(t)
	userID := createTestUser(t, s, "alice")
	_, err := s.CreateOrder(context.Background(), userID, "12345")
	require.NoError(t, err)
	accrual := 100.0
	require.NoError(t, s.UpdateOrderStatus(context.Background(), "12345", models.OrderStatusProcessed, &accrual))

	const attempts = 10
	var wg sync.WaitGroup
	errs := make([]error, attempts)
	for i := range attempts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = s.CreateWithdrawal(context.Background(), userID, "999", 100)
		}(i)
	}
	wg.Wait()

	var successes, insufficientFunds int
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, models.ErrInsufficientFunds):
			insufficientFunds++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, attempts-1, insufficientFunds)

	b, err := s.GetBalance(context.Background(), userID)
	require.NoError(t, err)
	require.InDelta(t, 0, b.Current, 0.001)
}

func TestGetWithdrawalsByUserID_OrderedNewestFirst(t *testing.T) {
	s := newTestStorage(t)
	userID := createTestUser(t, s, "alice")
	_, err := s.CreateOrder(context.Background(), userID, "12345")
	require.NoError(t, err)
	accrual := 500.0
	require.NoError(t, s.UpdateOrderStatus(context.Background(), "12345", models.OrderStatusProcessed, &accrual))

	require.NoError(t, s.CreateWithdrawal(context.Background(), userID, "111", 100))
	time.Sleep(10 * time.Millisecond)
	require.NoError(t, s.CreateWithdrawal(context.Background(), userID, "222", 100))

	withdrawals, err := s.GetWithdrawalsByUserID(context.Background(), userID)
	require.NoError(t, err)
	require.Len(t, withdrawals, 2)
	require.Equal(t, "222", withdrawals[0].Order)
	require.Equal(t, "111", withdrawals[1].Order)
}

func TestGetWithdrawalsByUserID_Empty(t *testing.T) {
	s := newTestStorage(t)
	userID := createTestUser(t, s, "alice")

	withdrawals, err := s.GetWithdrawalsByUserID(context.Background(), userID)
	require.NoError(t, err)
	require.Empty(t, withdrawals)
}

func TestGetOrdersForProcessing_FiltersNewAndProcessing(t *testing.T) {
	s := newTestStorage(t)
	userID := createTestUser(t, s, "alice")

	_, err := s.CreateOrder(context.Background(), userID, "111")
	require.NoError(t, err)
	_, err = s.CreateOrder(context.Background(), userID, "222")
	require.NoError(t, err)
	require.NoError(t, s.UpdateOrderStatus(context.Background(), "222", models.OrderStatusProcessing, nil))
	_, err = s.CreateOrder(context.Background(), userID, "333")
	require.NoError(t, err)
	accrual := 10.0
	require.NoError(t, s.UpdateOrderStatus(context.Background(), "333", models.OrderStatusProcessed, &accrual))
	_, err = s.CreateOrder(context.Background(), userID, "444")
	require.NoError(t, err)
	require.NoError(t, s.UpdateOrderStatus(context.Background(), "444", models.OrderStatusInvalid, nil))

	orders, err := s.GetOrdersForProcessing(context.Background(), 10)
	require.NoError(t, err)

	numbers := make([]string, len(orders))
	for i, o := range orders {
		numbers[i] = o.Number
	}
	require.ElementsMatch(t, []string{"111", "222"}, numbers)
}

func TestGetOrdersForProcessing_RespectsLimit(t *testing.T) {
	s := newTestStorage(t)
	userID := createTestUser(t, s, "alice")

	_, err := s.CreateOrder(context.Background(), userID, "111")
	require.NoError(t, err)
	_, err = s.CreateOrder(context.Background(), userID, "222")
	require.NoError(t, err)

	orders, err := s.GetOrdersForProcessing(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, orders, 1)
}
