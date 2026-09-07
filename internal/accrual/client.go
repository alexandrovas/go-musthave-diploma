package accrual

import (
	"context"
	"encoding/json"
	"errors"
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
	ErrOrderNotRegistered = errors.New("order not registered in accrual system")
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
		return nil, TooManyRequestsError{
			RetryAfter: c.getRetryAfter(resp),
		}

	default:
		return nil, fmt.Errorf("unexpected status code from accrual: %d", resp.StatusCode)
	}
}

// getRetryAfter вычисляет time.Duration из заголовка Retry-After.
// Если пришло невалидное значение или заголовок отсутствует, то возвращет 10s как fallback
func (c *Client) getRetryAfter(resp *http.Response) time.Duration {
	const (
		headerName = "Retry-After"
		fallback   = 10 * time.Second
	)

	logWrongHeader := func(val string) {
		msg := fmt.Sprintf("wrong value in response header %s, use default value %s as fallback", headerName, fallback)
		c.logger.Warn(msg, "value", val)
	}

	val := resp.Header.Get(headerName)
	if val != "" {
		if seconds, err := strconv.Atoi(val); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}

	logWrongHeader(val)
	return fallback
}
