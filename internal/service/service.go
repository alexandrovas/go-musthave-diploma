package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/alexandrovas/go-musthave-diploma/internal/models"
	"golang.org/x/crypto/bcrypt"
)

// Repository — интерфейс хранилища, определяемый на стороне потребителя (service)
type Repository interface {
	CreateUser(ctx context.Context, login, passwordHash string) (int64, error)
	GetUserByLogin(ctx context.Context, login string) (*models.User, error)
	CreateOrder(ctx context.Context, userID int64, number string) (*models.Order, error)
	GetOrderByNumberWithUserID(ctx context.Context, number string) (*models.Order, int64, error)
	GetOrdersByUserID(ctx context.Context, userID int64) ([]models.Order, error)
	UpdateOrderStatus(ctx context.Context, number string, status models.OrderStatus, accrual *float64) error
	GetBalance(ctx context.Context, userID int64) (*models.Balance, error)
	CreateWithdrawal(ctx context.Context, userID int64, orderNumber string, sum float64) error
	GetWithdrawalsByUserID(ctx context.Context, userID int64) ([]models.Withdrawal, error)
	GetOrdersForProcessing(ctx context.Context, limit int) ([]models.Order, error)
}

// OrderUploadStatus — константы результата загрузки заказа
type OrderUploadStatus int

const (
	UploadAccepted       OrderUploadStatus = iota // новый заказ принят в обработку
	UploadAlreadyByUser                           // заказ уже загружен этим пользователем
	UploadAlreadyByOther                          // заказ уже загружен другим пользователем
)

// GophermartService реализует бизнес-логику сервиса gophermart
type GophermartService struct {
	repo   Repository
	logger *slog.Logger
}

// NewGophermartService создаёт новый экземпляр сервиса
func NewGophermartService(repo Repository, logger *slog.Logger) *GophermartService {
	return &GophermartService{
		repo:   repo,
		logger: logger,
	}
}

// RegisterUser регистрирует нового пользователя и возвращает его id
func (s *GophermartService) RegisterUser(ctx context.Context, login, password string) (int64, error) {
	if login == "" || password == "" {
		return 0, fmt.Errorf("login and password must not be empty")
	}

	// Хеширование пароля через bcrypt
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, fmt.Errorf("hash password: %w", err)
	}

	id, err := s.repo.CreateUser(ctx, login, string(hash))
	if err != nil {
		return 0, err
	}

	return id, nil
}

// LoginUser аутентифицирует пользователя по паре логин/пароль и возвращает его id
func (s *GophermartService) LoginUser(ctx context.Context, login, password string) (int64, error) {
	user, err := s.repo.GetUserByLogin(ctx, login)
	if err != nil {
		if errors.Is(err, models.ErrUserNotFound) {
			return 0, models.ErrInvalidPassword
		}
		return 0, err
	}

	// Проверка пароля
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return 0, models.ErrInvalidPassword
	}

	return user.ID, nil
}

// UploadOrder загружает номер заказа для расчёта
func (s *GophermartService) UploadOrder(ctx context.Context, userID int64, orderNumber string) (OrderUploadStatus, error) {
	// Проверяем, существует ли уже заказ с таким номером
	existingOrder, ownerID, err := s.repo.GetOrderByNumberWithUserID(ctx, orderNumber)
	if err != nil && !errors.Is(err, models.ErrOrderNotFound) {
		return UploadAccepted, err
	}

	if existingOrder != nil {
		// Заказ уже существует
		if ownerID == userID {
			return UploadAlreadyByUser, nil
		}
		return UploadAlreadyByOther, nil
	}

	// Создаём новый заказ
	_, err = s.repo.CreateOrder(ctx, userID, orderNumber)
	if err != nil {
		return UploadAccepted, err
	}

	return UploadAccepted, nil
}

// GetOrders возвращает список заказов пользователя
func (s *GophermartService) GetOrders(ctx context.Context, userID int64) ([]models.Order, error) {
	return s.repo.GetOrdersByUserID(ctx, userID)
}

// GetBalance возвращает текущий баланс пользователя
func (s *GophermartService) GetBalance(ctx context.Context, userID int64) (*models.Balance, error) {
	return s.repo.GetBalance(ctx, userID)
}

// WithdrawBalance списывает баллы с накопительного счёта пользователя
func (s *GophermartService) WithdrawBalance(ctx context.Context, userID int64, orderNumber string, sum float64) error {
	return s.repo.CreateWithdrawal(ctx, userID, orderNumber, sum)
}

// GetWithdrawals возвращает историю списаний пользователя
func (s *GophermartService) GetWithdrawals(ctx context.Context, userID int64) ([]models.Withdrawal, error) {
	return s.repo.GetWithdrawalsByUserID(ctx, userID)
}
