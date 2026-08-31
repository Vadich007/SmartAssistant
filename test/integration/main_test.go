//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var dsn string

func TestMain(m *testing.M) {
	if external := os.Getenv("TEST_DATABASE_DSN"); external != "" {
		dsn = external
		os.Exit(m.Run())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("meetnotes_test"),
		tcpostgres.WithUsername("meetnotes"),
		tcpostgres.WithPassword("meetnotes"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(2*time.Minute),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"Пропуск интеграционных тестов: не удалось запустить PostgreSQL в Docker (%v).\n"+
				"Запустите Docker или задайте TEST_DATABASE_DSN для внешней тестовой базы.\n", err)
		os.Exit(0)
	}

	dsn, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "не удалось получить строку подключения: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	if err := testcontainers.TerminateContainer(container); err != nil {
		fmt.Fprintf(os.Stderr, "не удалось остановить контейнер: %v\n", err)
	}
	os.Exit(code)
}
