// Command workforce is the composition root for the Workforce Management
// service: it wires config from the environment to adapters, use cases, and
// the HTTP router, then serves.
package main

import (
	"context"
	"fmt"
	"log"
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
		log.Fatal(err)
	}
}

func run() error {
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

	publisher, closePublisher, err := newEventPublisher(shiftPlans)
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
		Handler:           inbound.NewRouter(handler),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("workforce-management listening on %s", httpAddr)
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
func newEventPublisher(shiftPlans ports.ShiftPlanRepo) (ports.EventPublisher, func(), error) {
	switch envOrDefault("EVENT_PUBLISHER", "log") {
	case "kafka":
		brokers := strings.Split(envOrDefault("KAFKA_BROKERS", "localhost:9092"), ",")
		pub := kafka.NewPublisher(brokers, shiftPlans)
		log.Printf("event publisher: kafka brokers=%v topic=%s", brokers, kafka.Topic)
		return pub, func() {
			if err := pub.Close(); err != nil {
				log.Printf("kafka publisher close: %v", err)
			}
		}, nil
	case "log":
		return events.NewLogPublisher(log.Default()), func() {}, nil
	default:
		return nil, nil, fmt.Errorf("unknown EVENT_PUBLISHER %q (want kafka or log)", os.Getenv("EVENT_PUBLISHER"))
	}
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("missing required env var %s", key)
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
		log.Fatalf("invalid float for env var %s: %v", key, err)
	}
	return f
}
