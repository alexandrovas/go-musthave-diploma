package retry

import (
	"context"
	"time"
)

// Intervals — интервалы между повторными попытками по умолчанию: 1s, 3s, 5s
// (итого 3 дополнительные попытки после первой неудачной)
var Intervals = []time.Duration{
	1 * time.Second,
	3 * time.Second,
	5 * time.Second,
}

// IsRetriable решает, стоит ли повторять операцию, вернувшую err
type IsRetriable func(err error) bool

// Do выполняет fn. Если fn вернула retriable-ошибку (по мнению isRetriable),
// попытка повторяется с задержками из intervals — итого len(intervals)
// дополнительных попыток. Возвращает nil при первом успехе, либо последнюю
// полученную ошибку, если все попытки исчерпаны или ошибка не retriable
// Если ctx отменяется во время ожидания между попытками, Do прерывается и
// возвращает ctx.Err()
func Do(ctx context.Context, isRetriable IsRetriable, intervals []time.Duration, fn func() error) error {
	err := fn()
	if err == nil || !isRetriable(err) {
		return err
	}

	for _, d := range intervals {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return ctx.Err()
		}

		err = fn()
		if err == nil || !isRetriable(err) {
			return err
		}
	}
	return err
}
