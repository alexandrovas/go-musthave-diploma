package accrual

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/alexandrovas/go-musthave-diploma/internal/retry"
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

// ServerError представляет ошибку 5xx от accrual-сервиса — временную проблему
// на стороне сервиса (перегрузка, недоступность), которую имеет смысл повторить
type ServerError struct {
	StatusCode int
}

func (e ServerError) Error() string {
	return fmt.Sprintf("accrual server error: status %d", e.StatusCode)
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

// GetOrderAccrual запрашивает информацию о начислении для заказа из accrual-сервиса.
// Запрос оборачивается в retry.Do: временные сбои сервиса (5xx) и сетевые ошибки
// повторяются автоматически, чтобы не терять заказ из-за кратковременной недоступности
func (c *Client) GetOrderAccrual(ctx context.Context, orderNumber string) (*Response, error) {
	url := fmt.Sprintf("%s/api/orders/%s", c.baseURL, orderNumber)

	var result *Response
	err := retry.Do(ctx, isRetriableAccrualError, retry.Intervals, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("creating request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("executing request: %w", err)
		}
		defer resp.Body.Close()

		switch {
		case resp.StatusCode == http.StatusOK:
			var accrualResp Response
			if err := json.NewDecoder(resp.Body).Decode(&accrualResp); err != nil {
				return fmt.Errorf("decoding response: %w", err)
			}
			result = &accrualResp
			return nil

		case resp.StatusCode == http.StatusNoContent:
			return ErrOrderNotRegistered

		case resp.StatusCode == http.StatusTooManyRequests:
			return TooManyRequestsError{
				RetryAfter: c.getRetryAfter(resp),
			}

		case resp.StatusCode >= http.StatusInternalServerError:
			return ServerError{StatusCode: resp.StatusCode}

		default:
			return fmt.Errorf("unexpected status code from accrual: %d", resp.StatusCode)
		}
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// isRetriableAccrualError сообщает, стоит ли повторить запрос к accrual-сервису.
// Retriable: 5xx-ответы сервиса (временная перегрузка/недоступность) и сетевые
// ошибки (обрыв соединения, таймаут). 429 сюда не входит — его обрабатывает
// вызывающий код отдельно, ориентируясь на заголовок Retry-After
func isRetriableAccrualError(err error) bool {
	if err == nil {
		return false
	}

	if _, ok := errors.AsType[ServerError](err); ok {
		return true
	}

	_, ok := errors.AsType[net.Error](err)
	return ok
}

// getRetryAfter вычисляет time.Duration из заголовка Retry-After.
// Если пришло невалидное значение или заголовок отсутствует, то возвращет 10s как fallback
func (c *Client) getRetryAfter(resp *http.Response) time.Duration {
	const (
		headerName = "Retry-After"
		fallback   = 10 * time.Second
	)

	val := resp.Header.Get(headerName)
	if val != "" {
		if seconds, err := strconv.Atoi(val); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}

		c.logger.Warn("wrong value in response header, use fallback value",
			"header", headerName, "fallback", fallback, "value", val)
	}

	return fallback
}
