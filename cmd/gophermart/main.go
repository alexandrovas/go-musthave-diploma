package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/alexandrovas/go-musthave-diploma/internal/cmd/gophermart"
	cmdHelper "github.com/alexandrovas/go-musthave-diploma/internal/cmd/gophermart/helper"
	"github.com/alexandrovas/go-musthave-diploma/internal/config"
)

func cmd() *cobra.Command {
	var configFile string

	cmd := &cobra.Command{
		Use:   "gophermart",
		Short: "gophermart - loyalty accumulation system",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig(configFile, cmd.Flags())
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			logger, err := cmdHelper.NewLogger(cfg.Log.Level, string(cfg.Log.Format))
			if err != nil {
				return fmt.Errorf("failed to setup logger: %w", err)
			}
			app := gophermart.New(cfg, logger)
			if err := app.Run(); err != nil {
				return err
			}
			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(&configFile, "config", "c", "config.yaml", "config file path")
	cmd.PersistentFlags().StringP("run_address", "a", "localhost:8080", "server listen address")
	cmd.PersistentFlags().StringP("database_uri", "d", "", "database connection URI")
	cmd.PersistentFlags().StringP("accrual_system_address", "r", "", "accrual system address")
	cmd.PersistentFlags().StringP("jwt_secret", "s", "gophermart-secret-key", "JWT signing secret")
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
