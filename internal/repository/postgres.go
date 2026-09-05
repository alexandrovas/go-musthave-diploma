package repository

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/alexandrovas/go-musthave-diploma/internal/models"
	"github.com/alexandrovas/go-musthave-diploma/internal/retry"
	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"

	// Регистрация pgx как драйвера database/sql
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/postgres/*.sql
var migrationsFS embed.FS

// PostgresStorage — хранилище данных на базе PostgreSQL
type PostgresStorage struct {
	logger *slog.Logger
	db     *sql.DB
}

// NewPostgresStorage открывает пул соединений к PostgreSQL по dsn и накатывает миграции
func NewPostgresStorage(ctx context.Context, logger *slog.Logger, dsn string) (*PostgresStorage, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	s := &PostgresStorage{
		logger: logger,
		db:     db,
	}

	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return s, nil
}

// Close закрывает пул соединений
func (s *PostgresStorage) Close() {
	s.db.Close()
}

// CreateUser создаёт нового пользователя.
// Возвращает id созданного пользователя
func (s *PostgresStorage) CreateUser(ctx context.Context, login, passwordHash string) (int64, error) {
	var id int64
	err := retry.Do(ctx, isRetriableDBError, retry.Intervals,
		func() error {
			const q = `INSERT INTO users (login, password) VALUES ($1, $2) RETURNING id`
			return s.db.QueryRowContext(ctx, q, login, passwordHash).Scan(&id)
		})
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == pgerrcode.UniqueViolation {
			return 0, models.ErrLoginTaken
		}
		return 0, fmt.Errorf("create user: %w", err)
	}
	return id, nil
}

// GetUserByLogin возвращает пользователя по логину
func (s *PostgresStorage) GetUserByLogin(ctx context.Context, login string) (*models.User, error) {
	var u models.User
	err := retry.Do(ctx, isRetriableDBError, retry.Intervals,
		func() error {
			const q = `SELECT id, login, password FROM users WHERE login = $1`
			scanErr := s.db.QueryRowContext(ctx, q, login).Scan(&u.ID, &u.Login, &u.Password)
			if errors.Is(scanErr, sql.ErrNoRows) {
				return models.ErrUserNotFound
			}
			return scanErr
		})
	if err != nil {
		return nil, fmt.Errorf("get user by login: %w", err)
	}
	return &u, nil
}

// CreateOrder создаёт новый заказ со статусом NEW.
// Возвращает заказ и флаг, был ли он уже создан
func (s *PostgresStorage) CreateOrder(ctx context.Context, userID int64, number string) (*models.Order, error) {
	var o models.Order
	var accrual sql.NullFloat64
	var uploadedAt time.Time

	err := retry.Do(ctx, isRetriableDBError, retry.Intervals,
		func() error {
			const q = `INSERT INTO orders (user_id, number, status) VALUES ($1, $2, 'NEW') RETURNING number, status, accrual, uploaded_at`
			scanErr := s.db.QueryRowContext(ctx, q, userID, number).Scan(&o.Number, &o.Status, &accrual, &uploadedAt)
			if scanErr != nil {
				return scanErr
			}
			o.UploadedAt = uploadedAt
			if accrual.Valid {
				o.Accrual = &accrual.Float64
			}
			return nil
		})
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, models.ErrOrderAlreadyExists
		}
		return nil, fmt.Errorf("create order: %w", err)
	}
	return &o, nil
}

// GetOrderByNumberWithUserID возвращает заказ по номеру вместе с userID владельца
func (s *PostgresStorage) GetOrderByNumberWithUserID(ctx context.Context, number string) (*models.Order, int64, error) {
	var o models.Order
	var userID int64
	var accrual sql.NullFloat64
	var uploadedAt time.Time

	err := retry.Do(ctx, isRetriableDBError, retry.Intervals,
		func() error {
			const q = `SELECT user_id, number, status, accrual, uploaded_at FROM orders WHERE number = $1`
			scanErr := s.db.QueryRowContext(ctx, q, number).Scan(&userID, &o.Number, &o.Status, &accrual, &uploadedAt)
			if errors.Is(scanErr, sql.ErrNoRows) {
				return models.ErrOrderNotFound
			}
			return scanErr
		})
	if err != nil {
		if errors.Is(err, models.ErrOrderNotFound) {
			return nil, 0, models.ErrOrderNotFound
		}
		return nil, 0, fmt.Errorf("get order by number with user id: %w", err)
	}

	o.UploadedAt = uploadedAt
	if accrual.Valid {
		o.Accrual = &accrual.Float64
	}
	return &o, userID, nil
}

// GetOrdersByUserID возвращает список заказов пользователя, отсортированных по времени загрузки (новые → старые)
func (s *PostgresStorage) GetOrdersByUserID(ctx context.Context, userID int64) ([]models.Order, error) {
	var orders []models.Order
	err := retry.Do(ctx, isRetriableDBError, retry.Intervals,
		func() error {
			const q = `SELECT number, status, accrual, uploaded_at FROM orders WHERE user_id = $1 ORDER BY uploaded_at DESC`
			rows, err := s.db.QueryContext(ctx, q, userID)
			if err != nil {
				return err
			}
			defer rows.Close()

			orders = nil // сброс на случай повторной попытки
			for rows.Next() {
				var o models.Order
				var accrual sql.NullFloat64
				if err := rows.Scan(&o.Number, &o.Status, &accrual, &o.UploadedAt); err != nil {
					return err
				}
				if accrual.Valid {
					o.Accrual = &accrual.Float64
				}
				orders = append(orders, o)
			}
			return rows.Err()
		})
	if err != nil {
		return nil, fmt.Errorf("get orders by user id: %w", err)
	}
	return orders, nil
}

// UpdateOrderStatus обновляет статус и начисление заказа
func (s *PostgresStorage) UpdateOrderStatus(ctx context.Context, number string, status models.OrderStatus, accrual *float64) error {
	return retry.Do(ctx, isRetriableDBError, retry.Intervals,
		func() error {
			const q = `UPDATE orders SET status = $2, accrual = $3 WHERE number = $1`
			_, err := s.db.ExecContext(ctx, q, number, status, accrual)
			if err != nil {
				return fmt.Errorf("update order status: %w", err)
			}
			return nil
		})
}

// GetBalance возвращает текущий баланс пользователя
func (s *PostgresStorage) GetBalance(ctx context.Context, userID int64) (*models.Balance, error) {
	var b models.Balance
	err := retry.Do(ctx, isRetriableDBError, retry.Intervals,
		func() error {
			const q = `
				SELECT
					COALESCE(SUM(o.accrual), 0),
					COALESCE(SUM(w.sum), 0)
				FROM users u
				LEFT JOIN orders o ON o.user_id = u.id AND o.status = 'PROCESSED'
				LEFT JOIN withdrawals w ON w.user_id = u.id
				WHERE u.id = $1
				GROUP BY u.id`
			scanErr := s.db.QueryRowContext(ctx, q, userID).Scan(&b.Current, &b.Withdrawn)
			if errors.Is(scanErr, sql.ErrNoRows) {
				// У пользователя ещё нет ни заказов, ни списаний — баланс нулевой
				b.Current = 0
				b.Withdrawn = 0
				return nil
			}
			return scanErr
		})
	if err != nil {
		return nil, fmt.Errorf("get balance: %w", err)
	}
	return &b, nil
}

// GetAvailableBalance возвращает доступный баланс (начисления минус списания)
func (s *PostgresStorage) GetAvailableBalance(ctx context.Context, userID int64) (float64, error) {
	var available float64
	err := retry.Do(ctx, isRetriableDBError, retry.Intervals,
		func() error {
			const q = `
				SELECT
					COALESCE(SUM(o.accrual), 0) - COALESCE(SUM(w.sum), 0)
				FROM users u
				LEFT JOIN orders o ON o.user_id = u.id AND o.status = 'PROCESSED'
				LEFT JOIN withdrawals w ON w.user_id = u.id
				WHERE u.id = $1
				GROUP BY u.id`
			scanErr := s.db.QueryRowContext(ctx, q, userID).Scan(&available)
			if errors.Is(scanErr, sql.ErrNoRows) {
				available = 0
				return nil
			}
			return scanErr
		})
	if err != nil {
		return 0, fmt.Errorf("get available balance: %w", err)
	}
	return available, nil
}

// CreateWithdrawal создаёт списание в рамках транзакции с проверкой баланса
func (s *PostgresStorage) CreateWithdrawal(ctx context.Context, userID int64, orderNumber string, sum float64) error {
	return retry.Do(ctx, isRetriableDBError, retry.Intervals,
		func() error {
			tx, err := s.db.BeginTx(ctx, nil)
			if err != nil {
				return fmt.Errorf("begin transaction: %w", err)
			}
			defer tx.Rollback()

			// Проверка доступного баланса в рамках транзакции
			var available float64
			const balanceQ = `
				SELECT
					COALESCE(SUM(o.accrual), 0) - COALESCE(SUM(w.sum), 0)
				FROM users u
				LEFT JOIN orders o ON o.user_id = u.id AND o.status = 'PROCESSED'
				LEFT JOIN withdrawals w ON w.user_id = u.id
				WHERE u.id = $1
				GROUP BY u.id`
			scanErr := tx.QueryRowContext(ctx, balanceQ, userID).Scan(&available)
			if scanErr != nil {
				if errors.Is(scanErr, sql.ErrNoRows) {
					available = 0
				} else {
					return fmt.Errorf("check balance: %w", scanErr)
				}
			}

			if available < sum {
				return models.ErrInsufficientFunds
			}

			// Вставка списания
			const insertQ = `INSERT INTO withdrawals (user_id, order_number, sum) VALUES ($1, $2, $3)`
			if _, err := tx.ExecContext(ctx, insertQ, userID, orderNumber, sum); err != nil {
				return fmt.Errorf("insert withdrawal: %w", err)
			}

			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit transaction: %w", err)
			}
			return nil
		})
}

// GetWithdrawalsByUserID возвращает историю списаний пользователя, отсортированную по времени (новые → старые)
func (s *PostgresStorage) GetWithdrawalsByUserID(ctx context.Context, userID int64) ([]models.Withdrawal, error) {
	var withdrawals []models.Withdrawal
	err := retry.Do(ctx, isRetriableDBError, retry.Intervals,
		func() error {
			const q = `SELECT order_number, sum, processed_at FROM withdrawals WHERE user_id = $1 ORDER BY processed_at DESC`
			rows, err := s.db.QueryContext(ctx, q, userID)
			if err != nil {
				return err
			}
			defer rows.Close()

			withdrawals = nil // сброс на случай повторной попытки
			for rows.Next() {
				var w models.Withdrawal
				if err := rows.Scan(&w.Order, &w.Sum, &w.ProcessedAt); err != nil {
					return err
				}
				withdrawals = append(withdrawals, w)
			}
			return rows.Err()
		})
	if err != nil {
		return nil, fmt.Errorf("get withdrawals by user id: %w", err)
	}
	return withdrawals, nil
}

// GetOrdersForProcessing возвращает заказы в статусе NEW или PROCESSING для фоновой обработки
func (s *PostgresStorage) GetOrdersForProcessing(ctx context.Context, limit int) ([]models.Order, error) {
	var orders []models.Order
	err := retry.Do(ctx, isRetriableDBError, retry.Intervals,
		func() error {
			const q = `
				SELECT number, status, accrual, uploaded_at
				FROM orders
				WHERE status IN ('NEW', 'PROCESSING')
				ORDER BY uploaded_at ASC
				LIMIT $1`
			rows, err := s.db.QueryContext(ctx, q, limit)
			if err != nil {
				return err
			}
			defer rows.Close()

			orders = nil // сброс на случай повторной попытки
			for rows.Next() {
				var o models.Order
				var accrual sql.NullFloat64
				if err := rows.Scan(&o.Number, &o.Status, &accrual, &o.UploadedAt); err != nil {
					return err
				}
				if accrual.Valid {
					o.Accrual = &accrual.Float64
				}
				orders = append(orders, o)
			}
			return rows.Err()
		})
	if err != nil {
		return nil, fmt.Errorf("get orders for processing: %w", err)
	}
	return orders, nil
}

// isRetriableDBError сообщает, стоит ли повторить операцию с БД
func isRetriableDBError(err error) bool {
	if err == nil {
		return false
	}

	// Проверяем специфичные ошибки postgres
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		if pgerrcode.IsConnectionException(pgErr.Code) ||
			pgerrcode.IsTransactionRollback(pgErr.Code) ||
			pgerrcode.IsInsufficientResources(pgErr.Code) {
			return true
		}
		if pgErr.Code == pgerrcode.CannotConnectNow {
			return true
		}
		return false
	}

	// Сетевые ошибки — повторимые
	_, ok := errors.AsType[net.Error](err)
	return ok
}

// migrate накатывает все неприменённые миграции из каталога migrations/postgres
func (s *PostgresStorage) migrate(_ context.Context) error {
	sourceDriver, err := iofs.New(migrationsFS, "migrations/postgres")
	if err != nil {
		return fmt.Errorf("create iofs source: %w", err)
	}
	defer sourceDriver.Close()

	dbDriver, err := pgxmigrate.WithInstance(s.db, &pgxmigrate.Config{})
	if err != nil {
		return fmt.Errorf("create db driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "pgx5", dbDriver)
	if err != nil {
		return fmt.Errorf("initialize migrator: %w", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}

	s.logger.Info("database migrations applied")
	return nil
}
