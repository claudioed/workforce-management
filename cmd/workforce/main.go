// Command workforce is the composition root for the Workforce Management
// service: it wires config from the environment to adapters, use cases, and
// the HTTP router, then serves.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	inbound "github.com/claudioed/workforce-management/internal/adapters/inbound/http"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/clock"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/events"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/kafka"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/postgres"
	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/application/usecases"
)

func main() {
	if err := run(); err != nil {
		slog.Error("service exited with error", "error", err)
		os.Exit(1)
	}
}

// newLogger builds the service's JSON-to-stdout slog.Logger. level accepts
// debug|info|warn|error (case-insensitive), defaulting to info for an
// unrecognized value.
func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}

func run() error {
	logger := newLogger(envOrDefault("LOG_LEVEL", "info"))
	slog.SetDefault(logger)

	databaseURL := requireEnv("DATABASE_URL")
	httpAddr := envOrDefault("HTTP_ADDR", ":8080")
	migrationsPath := envOrDefault("MIGRATIONS_PATH", "migrations")
	maxHoursPerShift := envFloatOrDefault("MAX_HOURS_PER_SHIFT", 8.0)

	if err := postgres.Migrate(databaseURL, migrationsPath); err != nil {
		return err
	}

	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	associates := postgres.NewAssociateRepo(pool)
	shiftPlans := postgres.NewShiftPlanRepo(pool)
	assignments := postgres.NewAssignmentRepo(pool)
	sysClock := clock.System{}

	publisher, closePublisher, err := newEventPublisher(shiftPlans, logger)
	if err != nil {
		return err
	}
	defer closePublisher()

	handler := &inbound.Handler{
		StartAssociateShift: &usecases.StartAssociateShift{Associates: associates, Events: publisher, Clock: sysClock},
		CertifyAssociate:    &usecases.CertifyAssociate{Associates: associates, Events: publisher, Clock: sysClock},
		ProposePathPlan:     &usecases.ProposePathPlan{Events: publisher, Clock: sysClock},
		CommitShiftPlan:     &usecases.CommitShiftPlan{ShiftPlans: shiftPlans, Events: publisher, Clock: sysClock, MaxHoursPerShift: maxHoursPerShift},
		AssignLabor:         &usecases.AssignLabor{Associates: associates, Assignments: assignments, Events: publisher, Clock: sysClock, MaxHoursPerShift: maxHoursPerShift},
		StartBreak:          &usecases.StartBreak{Associates: associates, Events: publisher, Clock: sysClock},
		EndBreak:            &usecases.EndBreak{Associates: associates, Events: publisher, Clock: sysClock},
		GetStaffingGap:      &usecases.GetStaffingGap{ShiftPlans: shiftPlans, Assignments: assignments, Events: publisher, Clock: sysClock},
		EndAssociateShift:   &usecases.EndAssociateShift{Associates: associates, Assignments: assignments, Events: publisher, Clock: sysClock, MaxHoursPerShift: maxHoursPerShift},
	}

	server := &http.Server{
		Addr:              httpAddr,
		Handler:           inbound.NewRouter(handler, logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", httpAddr)
		serverErr <- server.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
	case <-stop:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
	return nil
}

// newEventPublisher selects an EventPublisher via EVENT_PUBLISHER
// (kafka|log, default log) so existing behavior — and existing tests — are
// unaffected unless kafka is explicitly opted into. It returns a close func
// to release any adapter resources on shutdown.
func newEventPublisher(shiftPlans ports.ShiftPlanRepo, logger *slog.Logger) (ports.EventPublisher, func(), error) {
	switch envOrDefault("EVENT_PUBLISHER", "log") {
	case "kafka":
		brokers := strings.Split(envOrDefault("KAFKA_BROKERS", "localhost:9092"), ",")
		pub := kafka.NewPublisher(brokers, shiftPlans)
		logger.Info("event publisher configured", "publisher", "kafka", "brokers", brokers, "topic", kafka.Topic)
		return pub, func() {
			if err := pub.Close(); err != nil {
				logger.Error("kafka publisher close failed", "error", err)
			}
		}, nil
	case "log":
		return events.NewLogPublisher(logger), func() {}, nil
	default:
		return nil, nil, fmt.Errorf("unknown EVENT_PUBLISHER %q (want kafka or log)", os.Getenv("EVENT_PUBLISHER"))
	}
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("missing required env var", "key", key)
		os.Exit(1)
	}
	return v
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envFloatOrDefault(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		slog.Error("invalid float env var", "key", key, "error", err)
		os.Exit(1)
	}
	return f
}
