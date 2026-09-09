package accrual

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alexandrovas/go-musthave-diploma/internal/retry"
)

var testAccrualLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// withFastRetryIntervals временно заменяет retry.Intervals короткими задержками,
// чтобы тесты, проверяющие повторные попытки, не ждали реальные секунды
func withFastRetryIntervals(t *testing.T) {
	t.Helper()
	original := retry.Intervals
	retry.Intervals = []time.Duration{time.Millisecond, time.Millisecond}
	t.Cleanup(func() { retry.Intervals = original })
}

func TestGetOrderAccrual_Success(t *testing.T) {
	expected := &Response{Order: "123", Status: "PROCESSED", Accrual: float64Ptr(500)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewClient(server.URL, testAccrualLogger)
	resp, err := client.GetOrderAccrual(context.Background(), "123")
	require.NoError(t, err)
	require.Equal(t, "123", resp.Order)
	require.Equal(t, "PROCESSED", resp.Status)
	require.NotNil(t, resp.Accrual)
	require.InDelta(t, 500, *resp.Accrual, 0.01)
}

func TestGetOrderAccrual_NotRegistered(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, testAccrualLogger)
	_, err := client.GetOrderAccrual(context.Background(), "999")
	require.ErrorIs(t, err, ErrOrderNotRegistered)
}

func TestGetOrderAccrual_TooManyRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient(server.URL, testAccrualLogger)
	_, err := client.GetOrderAccrual(context.Background(), "123")
	var tooMany TooManyRequestsError
	require.ErrorAs(t, err, &tooMany)
	require.Equal(t, 30*time.Second, tooMany.RetryAfter)
}

func TestGetOrderAccrual_TooManyRequests_DefaultRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient(server.URL, testAccrualLogger)
	_, err := client.GetOrderAccrual(context.Background(), "123")
	var tooMany TooManyRequestsError
	require.ErrorAs(t, err, &tooMany)
	require.Equal(t, 10*time.Second, tooMany.RetryAfter)
}

func TestGetOrderAccrual_UnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewClient(server.URL, testAccrualLogger)
	_, err := client.GetOrderAccrual(context.Background(), "123")
	require.Error(t, err)
}

func TestGetOrderAccrual_ServerErrorRetriesThenSucceeds(t *testing.T) {
	withFastRetryIntervals(t)

	attempts := 0
	expected := &Response{Order: "123", Status: "PROCESSED", Accrual: float64Ptr(500)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewClient(server.URL, testAccrualLogger)
	resp, err := client.GetOrderAccrual(context.Background(), "123")
	require.NoError(t, err)
	require.Equal(t, 3, attempts)
	require.Equal(t, "PROCESSED", resp.Status)
}

func TestGetOrderAccrual_ServerErrorExhaustsRetries(t *testing.T) {
	withFastRetryIntervals(t)

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, testAccrualLogger)
	_, err := client.GetOrderAccrual(context.Background(), "123")
	var serverErr ServerError
	require.ErrorAs(t, err, &serverErr)
	require.Equal(t, http.StatusInternalServerError, serverErr.StatusCode)
	require.Equal(t, 1+len(retry.Intervals), attempts)
}

func TestTooManyRequestsError_Error(t *testing.T) {
	err := TooManyRequestsError{RetryAfter: 30 * time.Second}
	require.Contains(t, err.Error(), "30s")
}

func TestServerError_Error(t *testing.T) {
	err := ServerError{StatusCode: http.StatusBadGateway}
	require.Contains(t, err.Error(), "502")
}

func TestIsRetriableAccrualError(t *testing.T) {
	require.False(t, isRetriableAccrualError(nil))
	require.True(t, isRetriableAccrualError(ServerError{StatusCode: http.StatusServiceUnavailable}))
	require.False(t, isRetriableAccrualError(TooManyRequestsError{RetryAfter: time.Second}))
	require.False(t, isRetriableAccrualError(ErrOrderNotRegistered))
}

func float64Ptr(v float64) *float64 {
	return &v
}
