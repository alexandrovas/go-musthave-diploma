package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDo_SuccessOnFirstAttempt(t *testing.T) {
	err := Do(context.Background(), func(err error) bool { return true }, Intervals, func() error {
		return nil
	})
	require.NoError(t, err)
}

func TestDo_NonRetriableError(t *testing.T) {
	expected := errors.New("non-retriable")
	err := Do(context.Background(), func(err error) bool { return false }, Intervals, func() error {
		return expected
	})
	require.ErrorIs(t, err, expected)
}

func TestDo_SuccessAfterRetries(t *testing.T) {
	callCount := 0
	intervals := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}
	err := Do(context.Background(), func(err error) bool { return true }, intervals, func() error {
		callCount++
		if callCount < 3 {
			return errors.New("temporary error")
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 3, callCount)
}

func TestDo_AllRetriesExhausted(t *testing.T) {
	callCount := 0
	intervals := []time.Duration{10 * time.Millisecond}
	expected := errors.New("persistent error")
	err := Do(context.Background(), func(err error) bool { return true }, intervals, func() error {
		callCount++
		return expected
	})
	require.ErrorIs(t, err, expected)
	require.Equal(t, 2, callCount) // 1 initial + 1 retry
}

func TestDo_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	intervals := []time.Duration{1 * time.Second}
	err := Do(ctx, func(err error) bool { return true }, intervals, func() error {
		return errors.New("fail")
	})
	require.ErrorIs(t, err, context.Canceled)
}
