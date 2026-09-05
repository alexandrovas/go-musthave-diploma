CREATE TABLE IF NOT EXISTS users (
    id       BIGSERIAL PRIMARY KEY,
    login    TEXT    NOT NULL UNIQUE,
    password TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS orders (
    id          BIGSERIAL    PRIMARY KEY,
    user_id     BIGINT       NOT NULL REFERENCES users(id),
    number      TEXT         NOT NULL UNIQUE,
    status      TEXT         NOT NULL DEFAULT 'NEW',
    accrual     NUMERIC(20,2),
    uploaded_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_orders_user_id ON orders(user_id);
CREATE INDEX idx_orders_status_new_processing ON orders(status) WHERE status IN ('NEW', 'PROCESSING');

CREATE TABLE IF NOT EXISTS withdrawals (
    id           BIGSERIAL     PRIMARY KEY,
    user_id      BIGINT        NOT NULL REFERENCES users(id),
    order_number TEXT          NOT NULL,
    sum          NUMERIC(20,2) NOT NULL,
    processed_at TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_withdrawals_user_id ON withdrawals(user_id);
