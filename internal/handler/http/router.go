package handler

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/alexandrovas/go-musthave-diploma/internal/middleware"
)

// NewRouter создаёт и настраивает chi-роутер со всеми маршрутами и middleware
func NewRouter(svc GophermartService, logger *slog.Logger, jwtSecret string) http.Handler {
	h := NewHandler(svc, logger, jwtSecret)

	r := chi.NewRouter()

	// Глобальные middleware: логирование и сжатие
	r.Use(middleware.Logger(logger))
	r.Use(middleware.Compression)

	// Публичные маршруты (без аутентификации)
	r.Post("/api/user/register", h.RegisterUser)
	r.Post("/api/user/login", h.LoginUser)

	// Защищённые маршруты (требуют аутентификации)
	r.Route("/api/user", func(r chi.Router) {
		r.Use(middleware.Auth(jwtSecret))

		r.Post("/orders", h.UploadOrder)
		r.Get("/orders", h.GetOrders)

		r.Get("/balance", h.GetBalance)
		r.Post("/balance/withdraw", h.WithdrawBalance)
		r.Get("/withdrawals", h.GetWithdrawals)
	})

	return r
}
