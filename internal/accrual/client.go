package accrual

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// Response представляет ответ от accrual-сервиса
type Response struct {
	Order   string   `json:"order"`
	Status  string   `json:"status"`
	Accrual *float64 `json:"accrual,omitempty"`
}

// TooManyRequestsError представляет ошибку 429 от accrual-сервиса
type TooManyRequestsError struct {
	RetryAfter time.Duration
}

func (e TooManyRequestsError) Error() string {
	return fmt.Sprintf("too many requests, retry after %s", e.RetryAfter)
}

var (
	// ErrOrderNotRegistered — заказ не зарегистрирован в accrual-системе
	ErrOrderNotRegistered = fmt.Errorf("order not registered in accrual system")
	// ErrTooManyRequests — превышен лимит запросов к accrual-системе
	ErrTooManyRequests = fmt.Errorf("too many requests to accrual system")
)

// Client — HTTP-клиент для accrual-сервиса
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
}

// NewClient создаёт новый Client
func NewClient(baseURL string, logger *slog.Logger) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

// GetOrderAccrual запрашивает информацию о начислении для заказа из accrual-сервиса
func (c *Client) GetOrderAccrual(ctx context.Context, orderNumber string) (*Response, error) {
	url := fmt.Sprintf("%s/api/orders/%s", c.baseURL, orderNumber)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var accrualResp Response
		if err := json.NewDecoder(resp.Body).Decode(&accrualResp); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}
		return &accrualResp, nil

	case http.StatusNoContent:
		return nil, ErrOrderNotRegistered

	case http.StatusTooManyRequests:
		retryAfter := 60 * time.Second
		if val := resp.Header.Get("Retry-After"); val != "" {
			if seconds, err := strconv.Atoi(val); err == nil && seconds > 0 {
				retryAfter = time.Duration(seconds) * time.Second
			}
		}
		return nil, TooManyRequestsError{RetryAfter: retryAfter}

	default:
		return nil, fmt.Errorf("unexpected status code from accrual: %d", resp.StatusCode)
	}
}
