package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
)

// LogFormat определяет формат логирования (text или json)
type LogFormat string

const (
	JsonLogFormat LogFormat = "json"
	TextLogFormat LogFormat = "text"
)

// LogConfig представляет настройки логирования
type LogConfig struct {
	Level  string    `koanf:"level"`
	Format LogFormat `koanf:"format"`
}

// Config представляет конфигурацию сервиса gophermart
type Config struct {
	RunAddress           string    `koanf:"run_address"`
	DatabaseURI          string    `koanf:"database_uri"`
	AccrualSystemAddress string    `koanf:"accrual_system_address"`
	JWTSecret            string    `koanf:"jwt_secret"`
	Log                  LogConfig `koanf:"log"`
}

// LoadConfig загружает конфигурацию из файла, флагов CLI и переменных окружения
// Приоритет: файл < флаги < переменные окружения
func LoadConfig(configPath string, flags *pflag.FlagSet) (*Config, error) {
	k := koanf.New(".")

	// Загрузка из YAML-файла (отсутствие файла - не ошибка)
	f := file.Provider(configPath)
	if err := k.Load(f, yaml.Parser()); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("load config from file %s: %w", configPath, err)
		}
	}

	// Загрузка из CLI-флагов
	if err := k.Load(posflag.Provider(flags, ".", k), nil); err != nil {
		return nil, fmt.Errorf("load config from flags: %w", err)
	}

	// Загрузка из переменных окружения
	// Двойной underscore как разделитель вложенности, нижний регистр: RUN_ADDRESS → run.address
	if err := k.Load(env.Provider(".", env.Opt{
		TransformFunc: func(k, v string) (string, any) {
			k = strings.ReplaceAll(strings.ToLower(k), "__", ".")
			return k, v
		},
	}), nil); err != nil {
		return nil, fmt.Errorf("load config from env: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	// Валидация обязательных параметров
	if cfg.DatabaseURI == "" {
		return nil, fmt.Errorf("database URI is required (DATABASE_URI or -d)")
	}
	if cfg.AccrualSystemAddress == "" {
		return nil, fmt.Errorf("accrual system address is required (ACCRUAL_SYSTEM_ADDRESS or -r)")
	}

	// Значение по умолчанию для JWT-секрета
	if cfg.JWTSecret == "" {
		cfg.JWTSecret = "gophermart-secret-key"
	}

	return &cfg, nil
}
