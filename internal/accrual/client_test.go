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
)

var testAccrualLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

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
	require.Equal(t, 60*time.Second, tooMany.RetryAfter)
}

func TestGetOrderAccrual_UnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, testAccrualLogger)
	_, err := client.GetOrderAccrual(context.Background(), "123")
	require.Error(t, err)
}

func TestTooManyRequestsError_Error(t *testing.T) {
	err := TooManyRequestsError{RetryAfter: 30 * time.Second}
	require.Contains(t, err.Error(), "30s")
}

func float64Ptr(v float64) *float64 {
	return &v
}
