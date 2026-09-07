package gophermart

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/alexandrovas/go-musthave-diploma/internal/accrual"
	"github.com/alexandrovas/go-musthave-diploma/internal/config"
	handlerHttp "github.com/alexandrovas/go-musthave-diploma/internal/handler/http"
	"github.com/alexandrovas/go-musthave-diploma/internal/models"
	"github.com/alexandrovas/go-musthave-diploma/internal/repository"
	"github.com/alexandrovas/go-musthave-diploma/internal/service"
)

// App представляет приложение gophermart
type App struct {
	cfg    *config.Config
	logger *slog.Logger
}

// New создаёт новый экземпляр приложения
func New(cfg *config.Config, logger *slog.Logger) *App {
	return &App{
		cfg:    cfg,
		logger: logger,
	}
}

// Run запускает приложение: подключение к БД, запуск HTTP-сервера и воркера обработки заказов
func (a *App) Run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Подключение к БД
	repo, err := repository.NewPostgresStorage(ctx, a.logger, a.cfg.DatabaseURI)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer repo.Close()

	// Accrual-клиент
	accrualClient := accrual.NewClient(a.cfg.AccrualSystemAddress, a.logger)

	// Сервис бизнес-логики
	svc := service.NewGophermartService(repo, a.logger)

	// Роутер
	router := handlerHttp.NewRouter(svc, a.logger, a.cfg.JWTSecret)

	// HTTP-сервер
	server := &http.Server{
		Addr:    a.cfg.RunAddress,
		Handler: router,
	}

	// Воркер обработки заказов
	go a.processOrders(ctx, repo, accrualClient)

	// Graceful shutdown: при получении сигнала прерывания завершаем сервер
	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			a.logger.Error("server shutdown error", "error", err)
		}
	}()

	a.logger.Info("starting server", "address", a.cfg.RunAddress)
	err = server.ListenAndServe()
	cancel()

	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// processOrders — фоновый воркер, периодически опрашивающий accrual-сервис
// для обновления статусов заказов
func (a *App) processOrders(ctx context.Context, repo *repository.PostgresStorage, client *accrual.Client) {
	const tickInterval = 5 * time.Second
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	var retryAfter time.Duration

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		// Если мы в паузе после 429 Too Many Requests
		if retryAfter > 0 {
			a.logger.Info("waiting after rate limit", "retry_after", retryAfter)
			select {
			case <-ctx.Done():
				return
			case <-time.After(retryAfter):
				retryAfter = 0
			}
		}

		// Получаем заказы для обработки
		orders, err := repo.GetOrdersForProcessing(ctx, 10)
		if err != nil {
			a.logger.Error("failed to get orders for processing", "error", err)
			continue
		}

		if len(orders) == 0 {
			continue
		}

		// Обрабатываем каждый заказ
		for _, order := range orders {
			resp, err := client.GetOrderAccrual(ctx, order.Number)
			if err != nil {
				if tooManyReq, ok := errors.AsType[*accrual.TooManyRequestsError](err); ok {
					retryAfter = tooManyReq.RetryAfter
					a.logger.Warn("accrual rate limit hit", "retry_after", retryAfter)
					break // прервать итерацию
				}
				if errors.Is(err, accrual.ErrOrderNotRegistered) {
					a.logger.Debug("order not yet registered in accrual", "order", order.Number)
					continue
				}
				a.logger.Error("accrual request failed", "order", order.Number, "error", err)
				continue
			}

			// Маппинг статусов accrual → gophermart
			newStatus, accrual := mapAccrualStatus(resp)
			if newStatus != "" {
				if err := repo.UpdateOrderStatus(ctx, order.Number, newStatus, accrual); err != nil {
					a.logger.Error("failed to update order status", "order", order.Number, "error", err)
				}
			}
		}
	}
}

// mapAccrualStatus маппит статус accrual-сервиса в статус gophermart
func mapAccrualStatus(resp *accrual.Response) (models.OrderStatus, *float64) {
	switch resp.Status {
	case "REGISTERED":
		// Заказ зарегистрирован, но не обработан — оставляем NEW
		return models.OrderStatusNew, nil
	case "PROCESSING":
		return models.OrderStatusProcessing, nil
	case "INVALID":
		return models.OrderStatusInvalid, nil
	case "PROCESSED":
		return models.OrderStatusProcessed, resp.Accrual
	default:
		return "", nil
	}
}
