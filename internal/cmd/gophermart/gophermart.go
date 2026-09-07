package gophermart

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
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

	// Воркер обработки заказов. WaitGroup гарантирует, что воркер завершится
	// до defer repo.Close()
	var wg sync.WaitGroup
	wg.Go(func() {
		a.processOrders(ctx, repo, accrualClient)
	})
	defer wg.Wait()

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
	const (
		tickInterval = 10 * time.Second
		batchSize    = 10
	)
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
		orders, err := repo.GetOrdersForProcessing(ctx, batchSize)
		if err != nil {
			a.logger.Error("failed to get orders for processing", "error", err)
			continue
		}

		if len(orders) == 0 {
			continue
		}

		retryAfter = a.processBatch(ctx, repo, client, orders)
	}
}

// processBatch обрабатывает заказы батча параллельно — по горутине на заказ,
// вместо последовательных запросов. Это устраняет риск того, что при таймауте
// HTTP-клиента 10с и батче из 10 заказов один тик обработки растянется до
// 100с, что намного дольше интервала воркера (10с)
func (a *App) processBatch(ctx context.Context, repo *repository.PostgresStorage, client *accrual.Client, orders []models.Order) time.Duration {
	rateLimitCh := make(chan time.Duration, len(orders))

	var wg sync.WaitGroup
	for _, order := range orders {
		wg.Go(func() {
			a.processOrder(ctx, repo, client, order, rateLimitCh)
		})
	}
	wg.Wait()
	close(rateLimitCh)

	// Если хотя бы один запрос получает 429 Too Many Requests, остальные уже
	// запущенные горутины не прерываются (они и так почти все в процессе или уже
	// завершились к этому моменту) — вместо этого возвращается наибольшее из
	// полученных значений Retry-After, чтобы выдержать паузу перед следующим тиком
	var retryAfter time.Duration
	for d := range rateLimitCh {
		if d > retryAfter {
			retryAfter = d
		}
	}
	return retryAfter
}

// processOrder запрашивает начисление по одному заказу и обновляет его статус.
// При 429 значение Retry-After отправляется в rateLimitCh для агрегации в processBatch
func (a *App) processOrder(ctx context.Context, repo *repository.PostgresStorage, client *accrual.Client,
	order models.Order, rateLimitCh chan<- time.Duration) {
	resp, err := client.GetOrderAccrual(ctx, order.Number)
	if err != nil {
		if tooManyReq, ok := errors.AsType[accrual.TooManyRequestsError](err); ok {
			a.logger.Warn("accrual rate limit hit", "retry_after", tooManyReq.RetryAfter)
			rateLimitCh <- tooManyReq.RetryAfter
			return
		}
		if errors.Is(err, accrual.ErrOrderNotRegistered) {
			a.logger.Debug("order not yet registered in accrual", "order", order.Number)
			return
		}
		a.logger.Error("accrual request failed", "order", order.Number, "error", err)
		return
	}

	// Маппинг статусов accrual → gophermart
	newStatus, accrualAmount := mapAccrualStatus(resp)
	if newStatus != "" {
		if err := repo.UpdateOrderStatus(ctx, order.Number, newStatus, accrualAmount); err != nil {
			a.logger.Error("failed to update order status", "order", order.Number, "error", err)
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
