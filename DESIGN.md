# Дизайн-документ сервиса Gophermart

## 1. Обзор

Накопительная система лояльности «Гофермарт» — HTTP API сервис на Go, обеспечивающий:

- регистрацию и аутентификацию пользователей;
- приём номеров заказов и их асинхронную обработку через внешнюю систему расчёта начислений (accrual);
- ведение накопительного счёта баллов лояльности;
- списание баллов в счёт оплаты заказов.

Внешняя система расчёта начислений (accrual) не реализуется — используются готовые бинарники из `cmd/accrual/`. Сервис gophermart взаимодействует с accrual по HTTP.

---

## 2. Структура проекта

```
go-musthave-diploma/
  cmd/
    gophermart/
      main.go                  # CLI entry point (cobra)
    accrual/                   # Готовые бинарники accrual-сервиса (не реализуем)
      Dockerfile               # Docker-образ для запуска accrual (debian:bookworm-slim, platform: linux/amd64)
  internal/
    config/
      config.go                # Конфигурация (koanf: файл + env + flags)
    models/
      user.go                  # User, LoginRequest
      order.go                 # Order, OrderStatus
      balance.go               # Balance, Withdrawal, WithdrawRequest
      errors.go                # Sentinel-ошибки (ErrLoginTaken, ErrUserNotFound, etc.) + ErrorResponse
    repository/
      postgres.go              # PostgreSQL хранилище (pgx/v5 + golang-migrate)
      migrations/postgres/
        000001_init_schema.up.sql
        000001_init_schema.down.sql
    accrual/
      client.go                # HTTP-клиент для accrual-сервиса (отдельный пакет)
    service/
      service.go               # Бизнес-логика (GophermartService) + OrderUploadStatus
    handler/
      http/
        router.go              # Chi-роутер, регистрация маршрутов и middleware
        handler.go             # HTTP-хендлеры
        errors.go              # Sentinel-ошибки хендлеров
    middleware/
      auth.go                  # JWT-аутентификация (cookie), UserIDFromContext, SetAuthCookie
      jwt.go                   # generateJWT / validateJWT / BuildToken
      logger.go                # Логирование запросов
      compress.go              # Gzip-сжатие запросов/ответов
    retry/
      retry.go                 # Обобщённый retry-with-backoff
    luhn/
      luhn.go                  # Алгоритм Луна для валидации номеров заказов
    cmd/
      gophermart/
        gophermart.go          # Логика приложения (Run, graceful shutdown, accrual worker)
        helper/
          helper.go            # Фабрика логгера (NewLogger)
  docker-compose.yml            # postgres + postgres-accrual + accrual + accrual-init
  scripts/
    accrual-init.sh             # Инициализация тестовых данных в accrual
  go.mod
```

### Принципы организации кода

- **Слоёная архитектура**: `handler → service → repository`. Хендлеры — тонкие адаптеры, бизнес-логика — в service.
- **Interface-driven design**: интерфейс `service.Repository` определяется в пакете service, реализуется в repository (интерфейс на стороне потребителя).
- **Interface-on-consumer-side**: интерфейс `handler.GophermartService` определяется в пакете handler, реализуется service.
- Каждый пакет отвечает за свою область; зависимость направлена только внутрь (handler → service → repository).
- Взаимодействие с accrual вынесено в отдельный пакет `internal/accrual`.

---

## 3. Конфигурация сервиса

### Параметры

| Параметр                    | Env-var                  | Flag | По умолчанию            | Описание                                     |
|-----------------------------|--------------------------|------|-------------------------|----------------------------------------------|
| Адрес запуска сервиса       | `RUN_ADDRESS`            | `-a` | `localhost:8080`        | Адрес и порт запуска HTTP-сервера            |
| URI подключения к БД        | `DATABASE_URI`           | `-d` | `""` (обязательный)     | Строка подключения к PostgreSQL              |
| Адрес accrual-сервиса       | `ACCRUAL_SYSTEM_ADDRESS` | `-r` | `""` (обязательный)     | URL внешней системы расчёта начислений       |
| JWT-секрет                  | `JWT_SECRET`             | `-s` | `gophermart-secret-key` | Секрет для подписи JWT-токенов               |
| Уровень логирования         | `LOG_LEVEL`              | `-l` | `info`                  | Уровень лога (debug, info, warn, error)      |
| Формат лога                 | `LOG_FORMAT`             | `-f` | `text`                  | Формат лога (text, json)                     |
| Путь к файлу конфигурации   | —                        | `-c` | `config.yaml`           | Путь к YAML-файлу конфигурации               |

### Реализация

Используется библиотека **koanf** с трёхуровневой приоритизацией: **файл < CLI-флаги < переменные окружения**.

```go
type Config struct {
    RunAddress           string    `koanf:"run_address"`
    DatabaseURI          string    `koanf:"database_uri"`
    AccrualSystemAddress string    `koanf:"accrual_system_address"`
    JWTSecret            string    `koanf:"jwt_secret"`
    Log                  LogConfig `koanf:"log"`
}

type LogConfig struct {
    Level  string    `koanf:"level"`
    Format LogFormat `koanf:"format"`
}
```

**Пайплайн загрузки** (`LoadConfig`), приоритет возрастает сверху вниз — флаги перекрывают переменные окружения, которые перекрывают файл:
1. YAML-файл конфигурации через `file.Provider` + `yaml.Parser` (отсутствие файла — не ошибка).
2. Переменные окружения через `env.Provider` с трансформом: `RUN_ADDRESS` → `run_address` (нижний регистр, двойной underscore как разделитель вложенности).
3. CLI-флаги через `posflag.Provider` (spf13/pflag → koanf) — загружаются последними; провайдер подставляет значение флага по умолчанию только если ключ ещё не задан файлом/env, а явно переданные флаги (`f.Changed`) перекрывают их в любом случае.

**Валидация**: `LoadConfig` проверяет, что `DatabaseURI` и `AccrualSystemAddress` непустые. Если `JWTSecret` пуст — устанавливается значение по умолчанию `gophermart-secret-key`.

---

## 4. CLI (точка входа)

### Используем spf13/cobra

```go
// cmd/gophermart/main.go
func cmd() *cobra.Command {
    var configFile string
    cmd := &cobra.Command{
        Use:   "gophermart",
        Short: "gophermart - loyalty accumulation system",
        RunE: func(cmd *cobra.Command, args []string) error {
            cfg, err := config.LoadConfig(configFile, cmd.Flags())
            logger, err := helper.NewLogger(cfg.Log.Level, string(cfg.Log.Format))
            app := gophermart.New(cfg, logger)
            return app.Run()
        },
    }
    cmd.PersistentFlags().StringVarP(&configFile, "config", "c", "config.yaml", "config file path")
    cmd.PersistentFlags().StringP("run_address", "a", "localhost:8080", "server listen address")
    cmd.PersistentFlags().StringP("database_uri", "d", "", "database connection URI")
    cmd.PersistentFlags().StringP("accrual_system_address", "r", "", "accrual system address")
    cmd.PersistentFlags().StringP("jwt_secret", "s", "", "JWT signing secret")
    cmd.PersistentFlags().StringP("log.level", "l", "info", "log level (debug, info, warn, error)")
    cmd.PersistentFlags().StringP("log.format", "f", "text", "log format (text, json)")
    return cmd
}

func main() {
    cmd := cmd()
    if err := cmd.Execute(); err != nil {
        slog.Error(err.Error())
        os.Exit(2)
    }
}
```

---

## 5. Подключение к базе данных

### Используем pgx/v5 + golang-migrate

- Драйвер: `github.com/jackc/pgx/v5/stdlib` (blank import для регистрации).
- Подключение: `sql.Open("pgx", dsn)`.
- Миграции: `golang-migrate/v4` с встроенными SQL-файлами (`//go:embed migrations/postgres/*.sql`).
- Каждая DB-операция обёрнута в `retry.Do` с `isRetriableDBError` (коды PostgreSQL 08*, 40*, 53* + `net.Error`).
- Интервалы retry: `1s, 3s, 5s`.

### Схема БД

```sql
-- 000001_init_schema.up.sql

CREATE TABLE IF NOT EXISTS users (
    id       BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    login    VARCHAR(32) NOT NULL UNIQUE,
    password VARCHAR(72) NOT NULL
);

CREATE TYPE order_status AS ENUM ('NEW', 'PROCESSING', 'INVALID', 'PROCESSED');

CREATE TABLE IF NOT EXISTS orders (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users(id),
    number     VARCHAR(32) NOT NULL UNIQUE,
    status     order_status NOT NULL DEFAULT 'NEW',
    accrual    NUMERIC(20,2),
    uploaded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_orders_user_id ON orders(user_id);
CREATE INDEX idx_orders_status_new_processing ON orders(status) WHERE status IN ('NEW', 'PROCESSING');

CREATE TABLE IF NOT EXISTS withdrawals (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      BIGINT       NOT NULL REFERENCES users(id),
    order_number VARCHAR(32)  NOT NULL,
    sum          NUMERIC(20,2) NOT NULL,
    processed_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_withdrawals_user_id ON withdrawals(user_id);
```

**Обоснование типов**:
- `login` — `VARCHAR(32)`: разумный лимит для читаемого логина.
- `password` — хранится как bcrypt-хеш; лимит `VARCHAR(72)` соответствует максимальной длине пароля, которую учитывает bcrypt (72 байта), с запасом на смену алгоритма хеширования в будущем.
- `number`, `order_number` — номер заказа, строка цифр, проверяется алгоритмом Луна; `VARCHAR(32)` — с запасом относительно реальных номеров карт (до 19 цифр).
- `status` — ENUM `order_status` (`NEW`, `PROCESSING`, `INVALID`, `PROCESSED`) — фиксированный набор значений, СУБД гарантирует, что в колонку не попадёт ничего постороннего (в отличие от TEXT + CHECK, ещё и компактнее хранится).
- `accrual` — NUMERIC для точного хранения денежных значений; `NULL` пока начисление неизвестно.
- `uploaded_at`, `processed_at` — `TIMESTAMPTZ` для RFC3339-совместимого вывода.
- Частичный индекс `idx_orders_status_new_processing` — для эффективного выбора заказов, которые воркер должен отправлять в accrual.

---

## 6. Роутер

### Используем go-chi/chi/v5

```go
func NewRouter(svc handler.GophermartService, logger *slog.Logger, jwtSecret string) http.Handler {
    h := handler.NewHandler(svc, logger, jwtSecret)
    r := chi.NewRouter()

    // Глобальные middleware
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
```

---

## 7. Middleware

### 7.1. Auth (JWT в cookie)

- После успешной регистрации/логина генерируется JWT-токен (`middleware.BuildToken`) и устанавливается в cookie `token` (`middleware.SetAuthCookie`).
- Middleware `Auth(secret)` извлекает токен из cookie, валидирует подпись и срок действия через `validateJWT`.
- При успехе — `userID` кладётся в `r.Context()` через контекстный ключ `ContextKeyUserID`.
- При отсутствии/невалидности токена — `401 Unauthorized`.

```go
type contextKey string
const ContextKeyUserID contextKey = "userID"

func Auth(secret string) func(http.Handler) http.Handler { ... }
func UserIDFromContext(ctx context.Context) (int64, bool) { ... }
func SetAuthCookie(w http.ResponseWriter, token string) { ... }
```

### 7.2. JWT

- `BuildToken(userID int64, secret string) (string, error)` — генерация JWT с HMAC-SHA256, срок действия 24ч.
- `validateJWT(tokenString, secret string) (int64, error)` — валидация и извлечение `userID` из claims.
- Используется `golang-jwt/jwt/v5`.

### 7.3. Logger

Оборачивает `http.ResponseWriter` для захвата статус-кода и размера ответа. Логирует method, path, status, duration, size. Уровень лога: < 400 → Info, 4xx → Debug, 5xx → Error.

### 7.4. Compression

Прозрачное gzip-сжатие ответов (для `application/json`, `text/html`) и распаковка запросов с `Content-Encoding: gzip`.

Решение о сжатии принимается в `WriteHeader` на основе Content-Type и статус-кода (сжимаем только ответы < 300 с разрешённым Content-Type).

---

## 8. Handlers

### Структура хендлера

```go
type Handler struct {
    service   GophermartService
    logger    *slog.Logger
    jwtSecret string
}

func NewHandler(s GophermartService, logger *slog.Logger, jwtSecret string) *Handler {
    return &Handler{service: s, logger: logger, jwtSecret: jwtSecret}
}
```

### Интерфейс сервиса (на стороне потребителя)

```go
type GophermartService interface {
    RegisterUser(ctx context.Context, login, password string) (userID int64, err error)
    LoginUser(ctx context.Context, login, password string) (userID int64, err error)
    UploadOrder(ctx context.Context, userID int64, orderNumber string) (status service.OrderUploadStatus, err error)
    GetOrders(ctx context.Context, userID int64) ([]models.Order, error)
    GetBalance(ctx context.Context, userID int64) (*models.Balance, error)
    WithdrawBalance(ctx context.Context, userID int64, orderNumber string, sum float64) error
    GetWithdrawals(ctx context.Context, userID int64) ([]models.Withdrawal, error)
}
```

### Хендлеры

#### `POST /api/user/register` — RegisterUser

1. Декодировать JSON-тело `{login, password}`.
2. Валидация: непустые login и password.
3. Вызвать `service.RegisterUser(ctx, login, password)`.
4. При успехе — сгенерировать JWT (`middleware.BuildToken`), установить cookie `token` (`middleware.SetAuthCookie`), вернуть `200`.
5. Ошибки: `400` (неверный формат), `409` (логин занят — `ErrLoginTaken`), `500`.

#### `POST /api/user/login` — LoginUser

1. Декодировать JSON-тело `{login, password}`.
2. Валидация: непустые login и password.
3. Вызвать `service.LoginUser(ctx, login, password)`.
4. При успехе — сгенерировать JWT, установить cookie `token`, вернуть `200`.
5. Ошибки: `400` (неверный формат), `401` (неверная пара — `ErrInvalidPassword`), `500`.

#### `POST /api/user/orders` — UploadOrder

1. Извлечь `userID` из контекста (middleware Auth).
2. Прочитать тело как `text/plain` (номер заказа — строка цифр).
3. Валидация номера алгоритмом Луна (`luhn.IsValid(number)`). Неверный формат → `422`.
4. Вызвать `service.UploadOrder(ctx, userID, number)`.
5. Статус загрузки мапится в HTTP-коды:
   - `UploadAlreadyByUser` → `200`
   - `UploadAccepted` → `202`
   - `UploadAlreadyByOther` → `409`
6. Ошибки: `400` (пустое тело), `401`, `422`, `500`.

#### `GET /api/user/orders` — GetOrders

1. Извлечь `userID` из контекста.
2. Вызвать `service.GetOrders(ctx, userID)`.
3. Пустой результат → `204 No Content`.
4. Вернуть JSON-массив, отсортированный по `uploaded_at` (новые → старые).

#### `GET /api/user/balance` — GetBalance

1. Извлечь `userID` из контекста.
2. Вызвать `service.GetBalance(ctx, userID)`.
3. Вернуть JSON `{current, withdrawn}`.

#### `POST /api/user/balance/withdraw` — WithdrawBalance

1. Извлечь `userID` из контекста.
2. Декодировать JSON-тело `{order, sum}`.
3. Валидация: непустой order, `sum > 0`, номер заказа по Луну.
4. Вызвать `service.WithdrawBalance(ctx, userID, order, sum)`.
5. Ошибки: `401`, `402` (недостаточно средств — `ErrInsufficientFunds`), `422` (неверный номер заказа), `500`.

#### `GET /api/user/withdrawals` — GetWithdrawals

1. Извлечь `userID` из контекста.
2. Вызвать `service.GetWithdrawals(ctx, userID)`.
3. Пустой результат → `204 No Content`.
4. Вернуть JSON-массив, отсортированный по `processed_at` (новые → старые).

### Sentinel-ошибки хендлеров

```go
var (
    errInvalidRequestBody    = errors.New("invalid request body")
    errLoginTaken            = errors.New("login already taken")
    errInvalidCredentials    = errors.New("invalid login or password")
    errInvalidOrderNumber    = errors.New("invalid order number")
    errInsufficientFunds     = errors.New("insufficient funds")
    errOrderAlreadyUploaded  = errors.New("order already uploaded by another user")
    errInternal              = errors.New("internal server error")
)
```

---

## 9. Модели

```go
// models/order.go
type OrderStatus string

const (
    OrderStatusNew       OrderStatus = "NEW"
    OrderStatusProcessing OrderStatus = "PROCESSING"
    OrderStatusInvalid   OrderStatus = "INVALID"
    OrderStatusProcessed OrderStatus = "PROCESSED"
)

type Order struct {
    Number     string      `json:"number"`
    Status     OrderStatus `json:"status"`
    Accrual    *float64    `json:"accrual,omitempty"`
    UploadedAt time.Time   `json:"uploaded_at"`
}

// models/balance.go
type Balance struct {
    Current   float64 `json:"current"`
    Withdrawn float64 `json:"withdrawn"`
}

type Withdrawal struct {
    Order       string    `json:"order"`
    Sum         float64   `json:"sum"`
    ProcessedAt time.Time `json:"processed_at"`
}

type WithdrawRequest struct {
    Order string  `json:"order"`
    Sum   float64 `json:"sum"`
}

// models/user.go
type User struct {
    ID       int64
    Login    string
    Password string // bcrypt-хеш
}

type LoginRequest struct {
    Login    string `json:"login"`
    Password string `json:"password"`
}
```

---

## 10. Service (бизнес-логика)

### Интерфейс Repository (на стороне потребителя)

```go
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
```

### Реализация бизнес-логики

```go
type GophermartService struct {
    repo   Repository
    logger *slog.Logger
}
```

#### RegisterUser

1. Проверить, что логин и пароль непустые.
2. Хешировать пароль через `bcrypt.GenerateFromPassword`.
3. Создать пользователя (`repo.CreateUser`).
4. Вернуть `userID`.

#### LoginUser

1. Найти пользователя по логину (`repo.GetUserByLogin`).
2. Если `ErrUserNotFound` → вернуть `ErrInvalidPassword` (не раскрываем причину).
3. Проверить пароль через `bcrypt.CompareHashAndPassword`.
4. Вернуть `userID`.

#### UploadOrder

1. Найти заказ по номеру (`repo.GetOrderByNumberWithUserID`).
2. Если заказ не найден (`ErrOrderNotFound`) — создать заказ со статусом `NEW` (`repo.CreateOrder`), вернуть `UploadAccepted`.
3. Если заказ существует и принадлежит текущему пользователю → `UploadAlreadyByUser`.
4. Если заказ существует и принадлежит другому пользователю → `UploadAlreadyByOther`.

#### GetOrders / GetBalance / GetWithdrawals

Прокси-вызовы к repository.

#### WithdrawBalance

1. Создать списание (`repo.CreateWithdrawal`). Операция атомарна (транзакция внутри repository: проверка баланса + вставка списания в один tx, чтобы избежать race condition).
2. При недостатке средств → `ErrInsufficientFunds`.

---

## 11. Взаимодействие с accrual-сервисом

### Accrual-клиент (пакет `internal/accrual`)

```go
type Client struct {
    baseURL    string
    httpClient *http.Client
    logger     *slog.Logger
}

type Response struct {
    Order   string   `json:"order"`
    Status  string   `json:"status"`
    Accrual *float64 `json:"accrual,omitempty"`
}

type TooManyRequestsError struct {
    RetryAfter time.Duration
}

type ServerError struct {
    StatusCode int
}
```

Метод: `GetOrderAccrual(ctx context.Context, orderNumber string) (*Response, error)`

- Выполняет `GET {baseURL}/api/orders/{number}`.
- HTTP-клиент с таймаутом 10с.
- Запрос обёрнут в `retry.Do` с `isRetriableAccrualError` (5xx-ответы сервиса + `net.Error`) и стандартными интервалами `retry.Intervals` — временные сбои accrual-сервиса не приводят к потере заказа до следующего тика воркера.
- Обрабатывает коды ответа:
  - `200` — десериализовать JSON, вернуть результат.
  - `204` — заказ не зарегистрирован в accrual → вернуть `ErrOrderNotRegistered`.
  - `429` — прочитать `Retry-After` заголовок (по умолчанию 60с), вернуть `TooManyRequestsError` (не retriable — обрабатывается вызывающим кодом отдельно).
  - `5xx` — вернуть `ServerError` (retriable).
  - остальные — вернуть ошибку с кодом статуса (не retriable).

### Фоновый воркер обработки заказов

Запускается в `internal/cmd/gophermart/gophermart.go` в методе `Run()`.

**Алгоритм**:

1. Периодически (каждые 10с) выбирать до 10 заказов в статусе `NEW` или `PROCESSING` из БД (`repo.GetOrdersForProcessing`).
2. Заказы батча обрабатываются **параллельно** — по горутине на заказ (`processBatch` / `processOrder`), а не последовательно. При последовательной обработке таймаут HTTP-клиента 10с и батч из 10 заказов могли растянуть один тик до 100с — намного дольше интервала воркера (10с); параллельная обработка ограничивает время тика временем одного самого медленного запроса.
3. Для каждого заказа (`processOrder`):
   a. Запросить accrual-клиент: `accrualClient.GetOrderAccrual(ctx, order.Number)`.
   b. При `TooManyRequestsError` — не прерывать остальные уже запущенные горутины (они всё равно почти все в процессе или уже завершились), а отправить `RetryAfter` в буферизованный канал `rateLimitCh` для агрегации.
   c. При `ErrOrderNotRegistered` (204) — пропустить, статус остаётся `NEW`; штатная ситуация, логируется на уровне Debug (не Error), чтобы не засорять лог.
   d. Маппинг статусов accrual → gophermart:
      - `REGISTERED` → `NEW` (оставляем как есть)
      - `PROCESSING` → `PROCESSING`
      - `INVALID` → `INVALID` (с `accrual = nil`)
      - `PROCESSED` → `PROCESSED` (с accrual из ответа)
   e. Обновить заказ в БД: `repo.UpdateOrderStatus(ctx, number, newStatus, accrual)`.
4. `processBatch` дожидается завершения всех горутин батча (`sync.WaitGroup`), затем берёт максимальное значение `Retry-After` из `rateLimitCh` (если хотя бы одна горутина получила 429) — это значение возвращается наверх и используется для паузы перед следующим тиком.
5. Воркер работает в отдельной горутине, завершается по `ctx.Done()`.

**Обработка 429**: при получении 429 хотя бы от одного заказа батча воркер выдерживает паузу `Retry-After` (по умолчанию 60с, берётся максимум среди всех сработавших горутин) перед следующим тиком. Это предотвращает превышение лимита запросов.

---

## 12. Алгоритм Луна

```go
// internal/luhn/luhn.go
package luhn

func IsValid(number string) bool {
    // Проверка: только цифры, длина >= 1
    // Классический алгоритм Луна:
    // Проходим справа налево, каждую вторую цифру умножаем на 2,
    // если результат > 9, вычитаем 9. Сумма должна делиться на 10.
}
```

---

## 13. Repository (PostgreSQL)

### Storage

```go
type PostgresStorage struct {
    db     *sql.DB
    logger *slog.Logger
}

func NewPostgresStorage(ctx context.Context, logger *slog.Logger, dsn string) (*PostgresStorage, error)
```

### Конструктор

1. `sql.Open("pgx", dsn)` — открыть пул соединений.
2. `db.PingContext(ctx)` — проверить доступность.
3. Запустить миграции (`golang-migrate` с embedded SQL).
4. Вернуть `*PostgresStorage`.

### Ключевые SQL-операции

#### CreateUser

```sql
INSERT INTO users (login, password) VALUES ($1, $2) RETURNING id
```
При `unique_violation` (код 23505) → `ErrLoginTaken`.

#### GetUserByLogin

```sql
SELECT id, login, password FROM users WHERE login = $1
```

#### CreateOrder

```sql
INSERT INTO orders (user_id, number, status) VALUES ($1, $2, 'NEW') RETURNING number, status, accrual, uploaded_at
```

#### GetOrderByNumberWithUserID

```sql
SELECT o.id, o.number, o.status, o.accrual, o.uploaded_at, o.user_id
FROM orders o WHERE o.number = $1
```

Возвращает заказ и `user_id` владельца. Если не найден → `ErrOrderNotFound`.

#### GetOrdersForProcessing

```sql
SELECT id, user_id, number, status, accrual, uploaded_at
FROM orders
WHERE status IN ('NEW', 'PROCESSING')
ORDER BY uploaded_at ASC
LIMIT $1
```

#### UpdateOrderStatus

```sql
UPDATE orders SET status = $2, accrual = $3 WHERE number = $1
```

#### GetBalance

Используются независимые подзапросы вместо JOIN (чтобы избежать cross-product при нескольких заказах и списаниях):

```sql
SELECT
    (SELECT COALESCE(SUM(accrual), 0) FROM orders WHERE user_id = $1 AND status = 'PROCESSED')
    - (SELECT COALESCE(SUM(sum), 0) FROM withdrawals WHERE user_id = $1),
    (SELECT COALESCE(SUM(sum), 0) FROM withdrawals WHERE user_id = $1)
```

Всегда возвращает ровно одну строку (COALESCE гарантирует 0 при отсутствии данных).

#### CreateWithdrawal (в транзакции с блокировкой строки пользователя)

```sql
BEGIN;
-- Блокировка строки пользователя для предотвращения race condition
-- при параллельных списаниях: вторая транзакция дождётся завершения первой
SELECT id FROM users WHERE id = $1 FOR UPDATE;
-- Проверить баланс
SELECT
    (SELECT COALESCE(SUM(accrual), 0) FROM orders WHERE user_id = $1 AND status = 'PROCESSED')
    - (SELECT COALESCE(SUM(sum), 0) FROM withdrawals WHERE user_id = $1) AS available;
-- Если available >= sum:
INSERT INTO withdrawals (user_id, order_number, sum) VALUES ($1, $2, $3);
COMMIT;
```

При недостатке средств → `ErrInsufficientFunds`.

**Почему блокировка строки**: в `READ COMMITTED` (default) параллельные транзакции не видят незакоммиченные изменения друг друга — два tx могут одновременно увидеть `available=700` и оба разрешить списание. `SELECT ... FOR UPDATE` по строке пользователя в `users` сериализует списания для одного пользователя: вторая транзакция ждёт, пока первая не завершится (COMMIT/ROLLBACK снимает блокировку).

---

## 14. Приложение (Run + Graceful Shutdown)

```go
// internal/cmd/gophermart/gophermart.go
type App struct {
    cfg    *config.Config
    logger *slog.Logger
}

func New(cfg *config.Config, logger *slog.Logger) *App

func (a *App) Run() error {
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()

    // 1. Подключение к БД
    repo, err := repository.NewPostgresStorage(ctx, a.logger, a.cfg.DatabaseURI)
    defer repo.Close()

    // 2. Accrual-клиент
    accrualClient := accrual.NewClient(a.cfg.AccrualSystemAddress, a.logger)

    // 3. Service
    svc := service.NewGophermartService(repo, a.logger)

    // 4. Роутер
    router := handlerHttp.NewRouter(svc, a.logger, a.cfg.JWTSecret)

    // 5. HTTP-сервер
    server := &http.Server{Addr: a.cfg.RunAddress, Handler: router}

    // 6. Воркер обработки заказов
    var wg sync.WaitGroup
    wg.Go(func() {
        a.processOrders(ctx, repo, accrualClient)
    })
    defer wg.Wait()

    // 7. Graceful shutdown
    go func() {
        <-ctx.Done()
        shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer shutdownCancel()
        server.Shutdown(shutdownCtx)
    }()

    a.logger.Info("starting server", "address", a.cfg.RunAddress)
    err = server.ListenAndServe()
    cancel()
    if !errors.Is(err, http.ErrServerClosed) {
        return err
    }
    return nil
}
```

### processOrders / processBatch / processOrder — цикл воркера

```go
func (a *App) processOrders(ctx context.Context, repo *repository.PostgresStorage, client *accrual.Client) {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    var retryAfter time.Duration

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
        }

        // Если мы в паузе после 429
        if retryAfter > 0 {
            select {
            case <-ctx.Done():
                return
            case <-time.After(retryAfter):
                retryAfter = 0
            }
        }

        orders, err := repo.GetOrdersForProcessing(ctx, 10)
        if err != nil {
            continue
        }
        if len(orders) == 0 {
            continue
        }

        retryAfter = a.processBatch(ctx, repo, client, orders)
    }
}

// Заказы батча обрабатываются параллельно — по горутине на заказ
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

    var retryAfter time.Duration
    for d := range rateLimitCh {
        if d > retryAfter {
            retryAfter = d
        }
    }
    return retryAfter
}

func (a *App) processOrder(ctx context.Context, repo *repository.PostgresStorage, client *accrual.Client, order models.Order, rateLimitCh chan<- time.Duration) {
    resp, err := client.GetOrderAccrual(ctx, order.Number)
    if err != nil {
        if tooManyReq, ok := errors.AsType[accrual.TooManyRequestsError](err); ok {
            rateLimitCh <- tooManyReq.RetryAfter
            return
        }
        if errors.Is(err, accrual.ErrOrderNotRegistered) {
            // штатная ситуация: заказ ещё не принят accrual в обработку
            return
        }
        return
    }

    newStatus, accrualAmount := mapAccrualStatus(resp)
    if newStatus != "" {
        repo.UpdateOrderStatus(ctx, order.Number, newStatus, accrualAmount)
    }
}
```

---

## 15. Retry

```go
// internal/retry/retry.go
type IsRetriable func(error) bool

func Do(ctx context.Context, isRetriable IsRetriable, intervals []time.Duration, fn func() error) error
```

- Вызывает `fn()`. При успехе — возврат.
- При retriable-ошибке — ожидание интервала, повтор.
- При non-retriable-ошибке — немедленный возврат.
- Учитывает `ctx.Done()` для отмены.
- Интервалы по умолчанию: `1s, 3s, 5s`.

---

## 16. Логирование

- Используется `log/slog` (structured logging, Go 1.21+).
- Фабрика: `helper.NewLogger(level, format)` → `slog.NewTextHandler` или `slog.NewJSONHandler` в `os.Stdout`.
- Уровни: `debug`, `info`, `warn`, `error`.
- В тестах: `slog.New(slog.NewTextHandler(io.Discard, nil))`.
- Все лог-сообщения на английском.

---

## 17. Docker Compose

```yaml
services:
  postgres:               # БД gophermart (порт 5432)
  postgres-accrual:       # БД accrual (порт 5433)
  accrual:                # accrual-сервис (порт 8080, platform: linux/amd64)
  accrual-init:           # one-shot инициализация тестовых данных
```

### Запуск gophermart

Сервис gophermart запускается из консоли:

```bash
go run ./cmd/gophermart \
  -a :8081 \
  -d "postgres://postgres:postgres@localhost:5432/gophermart?sslmode=disable" \
  -r http://localhost:8080
```

---

## 18. Сводная таблица маршрутов

| Метод | Путь                        | Auth | Content-Type (Request) | Описание                    |
|-------|-----------------------------|------|------------------------|-----------------------------|
| POST  | `/api/user/register`        | Нет  | `application/json`    | Регистрация пользователя    |
| POST  | `/api/user/login`           | Нет  | `application/json`    | Аутентификация пользователя |
| POST  | `/api/user/orders`          | Да   | `text/plain`          | Загрузка номера заказа      |
| GET   | `/api/user/orders`          | Да   | —                      | Список заказов пользователя |
| GET   | `/api/user/balance`         | Да   | —                      | Баланс пользователя         |
| POST  | `/api/user/balance/withdraw`| Да   | `application/json`    | Списание баллов             |
| GET   | `/api/user/withdrawals`     | Да   | —                      | История списаний            |
