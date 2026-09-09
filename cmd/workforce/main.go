// Command workforce is the composition root for the Workforce Management
// service: it wires config from the environment to adapters, use cases, and
// the HTTP router, then serves.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/workforce-management/internal/adapters/inbound/auth"
	inbound "github.com/claudioed/workforce-management/internal/adapters/inbound/http"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/clock"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/events"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/filecatalog"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/fulfillmentexecution"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/kafka"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/kafkacatalog"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/laborperformance"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/postgres"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/telemetry"
	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/application/usecases"
)

// version is the service version reported as the OTel `service.version`
// resource attribute. It is overridable at build time
// (-ldflags "-X main.version=1.2.3") and otherwise falls back to the
// SERVICE_VERSION env var, then "dev".
var version = ""

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
	// TraceHandler wraps the JSON handler so any *Context log call made while
	// a span is active also carries trace_id/span_id.
	return slog.New(telemetry.NewTraceHandler(
		slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}),
	))
}

// serviceVersion resolves service.version: build-time ldflags first, then
// SERVICE_VERSION, then "dev".
func serviceVersion() string {
	if version != "" {
		return version
	}
	return envOrDefault("SERVICE_VERSION", "dev")
}

func run() error {
	logger := newLogger(envOrDefault("LOG_LEVEL", "info"))
	slog.SetDefault(logger)

	ctx := context.Background()

	serviceName := envOrDefault("OTEL_SERVICE_NAME", inbound.DefaultServiceName)
	otlpEndpoint := envOrDefault("OTEL_EXPORTER_OTLP_ENDPOINT", telemetry.DefaultOTLPEndpoint)
	shutdownTelemetry, err := telemetry.Setup(ctx, serviceName, serviceVersion(), otlpEndpoint)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Warn, not Error: the usual cause is "no Collector listening at
		// OTEL_EXPORTER_OTLP_ENDPOINT", which drops the final flush but is
		// not a service failure.
		if err := shutdownTelemetry(shutdownCtx); err != nil {
			logger.Warn("telemetry shutdown did not flush cleanly", "error", err)
		}
	}()
	logger.Info("telemetry configured",
		"service_name", serviceName,
		"service_version", serviceVersion(),
		"environment", telemetry.Environment(),
		"otlp_endpoint", otlpEndpoint,
	)

	databaseURL := requireEnv("DATABASE_URL")
	httpAddr := envOrDefault("HTTP_ADDR", ":8080")
	migrationsPath := envOrDefault("MIGRATIONS_PATH", "migrations")
	maxHoursPerShift := envFloatOrDefault("MAX_HOURS_PER_SHIFT", 8.0)

	// The process-path catalogue's SOURCE is selectable, defaulting to
	// the existing boot-time file read ("file") -- zero behavior change
	// for any existing deployment unless PATH_CATALOGUE_SOURCE=kafka is
	// explicitly set, matching this fleet's EVENT_PUBLISHER convention.
	// See internal/adapters/outbound/kafkacatalog's package doc comment
	// for the full rationale and the readiness-gate design, mirrored
	// byte-for-byte from fulfillment-execution's and
	// wes-work-planning's identical wiring.
	catalogueSource := envOrDefault("PATH_CATALOGUE_SOURCE", "file")

	var catalogue ports.PathCatalogue
	var kafkaCatalogue *kafkacatalog.Consumer
	// catalogueConsumerCtx/cancelCatalogueConsumer are declared here
	// (rather than deferred to later in run()) because the Kafka
	// catalogue source needs its own Run goroutine started BEFORE
	// WaitReady is called below -- otherwise nothing would ever be
	// consuming messages while this process waits, guaranteeing a
	// deadlock until WaitReadyTimeout.
	catalogueConsumerCtx, cancelCatalogueConsumer := context.WithCancel(context.Background())
	defer cancelCatalogueConsumer()

	switch catalogueSource {
	case "kafka":
		kafkaBrokersCSV := os.Getenv("KAFKA_BROKERS")
		if kafkaBrokersCSV == "" {
			return fmt.Errorf("PATH_CATALOGUE_SOURCE=kafka requires KAFKA_BROKERS to be set")
		}
		var err error
		kafkaCatalogue, err = kafkacatalog.NewConsumer(ctx, strings.Split(kafkaBrokersCSV, ","), logger)
		if err != nil {
			return fmt.Errorf("failed to start the Kafka-sourced process-path catalogue: %w", err)
		}
		logger.Info("process-path catalogue source configured", "source", "kafka", "topic", kafkacatalog.Topic)
		go func() {
			logger.Info("process-path catalogue consumer running", "topic", kafkacatalog.Topic)
			if err := kafkaCatalogue.Run(catalogueConsumerCtx); err != nil {
				logger.Error("process-path catalogue consumer stopped", "error", err)
			}
		}()

		logger.Info("waiting for the process-path catalogue to replay its initial history before accepting traffic")
		waitCtx, waitCancel := context.WithTimeout(context.Background(), kafkacatalog.WaitReadyTimeout)
		err = kafkaCatalogue.WaitReady(waitCtx)
		waitCancel()
		if err != nil {
			return fmt.Errorf("process-path catalogue did not become ready within %s: %w", kafkacatalog.WaitReadyTimeout, err)
		}
		logger.Info("process-path catalogue is ready", "paths", kafkaCatalogue.Ids())
		catalogue = kafkaCatalogue
	default:
		// The process-path catalogue is loaded and validated once at
		// boot, before anything else stands up — a missing or
		// malformed catalogue file must stop this service from
		// starting at all, never fall back to a partial/empty
		// catalogue (mirrors fulfillment-execution's and
		// wes-work-planning's identical boot-time contract; see
		// ADR-0013).
		fileCatalogue, err := filecatalog.Load(envOrDefault("PATH_CATALOGUE_FILE", "/etc/workforce-management/process-paths.yaml"))
		if err != nil {
			return fmt.Errorf("failed to load the process-path catalogue: %w", err)
		}
		logger.Info("process-path catalogue loaded", "paths", fileCatalogue.Ids())
		catalogue = fileCatalogue
	}

	if err := postgres.Migrate(databaseURL, migrationsPath); err != nil {
		return err
	}

	pool, err := postgres.NewPool(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	associates := postgres.NewAssociateRepo(pool)
	shiftPlans := postgres.NewShiftPlanRepo(pool)
	assignments := postgres.NewAssignmentRepo(pool)
	sysClock := clock.System{}

	publisher, relay, closePublisher, err := newEventPublisher(pool, shiftPlans, logger)
	if err != nil {
		return err
	}
	defer closePublisher()
	// Every publishing use case shares one UnitOfWork so its Saves and its
	// Publish (an outbox INSERT in kafka mode) commit together (ADR 0016).
	// The log publisher has nothing to bind, but bracketing the Saves in a
	// transaction is still correct, so the UnitOfWork is wired unconditionally.
	uow := postgres.NewUnitOfWork(pool)

	measuredRate := buildMeasuredRateClient(envOrDefault("LABOR_PERFORMANCE_MODE", "permissive"), os.Getenv("LABOR_PERFORMANCE_BASE_URL"), os.Getenv("LABOR_PERFORMANCE_API_KEY"), logger)
	installedCapacity := buildInstalledCapacityClient(envOrDefault("INSTALLED_CAPACITY_MODE", "permissive"), os.Getenv("FULFILLMENT_EXECUTION_BASE_URL"), os.Getenv("FULFILLMENT_EXECUTION_API_KEY"), logger)

	authn, authMode := configureAuth(os.Getenv, logger)

	handler := &inbound.Handler{
		StartAssociateShift: &usecases.StartAssociateShift{Associates: associates, Events: publisher, Clock: sysClock, UnitOfWork: uow},
		CertifyAssociate:    &usecases.CertifyAssociate{Associates: associates, Events: publisher, Clock: sysClock, UnitOfWork: uow},
		ProposePathPlan:     &usecases.ProposePathPlan{Events: publisher, Clock: sysClock, MeasuredRate: measuredRate, UnitOfWork: uow},
		CommitShiftPlan:     &usecases.CommitShiftPlan{ShiftPlans: shiftPlans, Events: publisher, Clock: sysClock, InstalledCapacity: installedCapacity, MaxHoursPerShift: maxHoursPerShift, UnitOfWork: uow},
		AssignLabor:         &usecases.AssignLabor{Associates: associates, Assignments: assignments, Events: publisher, Clock: sysClock, MaxHoursPerShift: maxHoursPerShift, UnitOfWork: uow},
		StartBreak:          &usecases.StartBreak{Associates: associates, Events: publisher, Clock: sysClock, UnitOfWork: uow},
		EndBreak:            &usecases.EndBreak{Associates: associates, Events: publisher, Clock: sysClock, UnitOfWork: uow},
		GetStaffingGap:      &usecases.GetStaffingGap{ShiftPlans: shiftPlans, Assignments: assignments, Events: publisher, Clock: sysClock, UnitOfWork: uow},
		EndAssociateShift:   &usecases.EndAssociateShift{Associates: associates, Assignments: assignments, Events: publisher, Clock: sysClock, MaxHoursPerShift: maxHoursPerShift, UnitOfWork: uow},
		Catalogue:           catalogue,
	}

	server := &http.Server{
		Addr:              httpAddr,
		Handler:           inbound.NewRouter(handler, logger, serviceName, inbound.WithAuth(authn, authMode)),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", httpAddr)
		serverErr <- server.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	// The outbox relay (ADR 0016) runs alongside the HTTP server in the
	// same process, draining outbox_events onto both Kafka topics. It is
	// only wired in kafka mode (see newEventPublisher).
	relayDone := make(chan struct{})
	relayCtx, stopRelay := context.WithCancel(context.Background())
	defer stopRelay()
	if relay != nil {
		go func() {
			defer close(relayDone)
			logger.Info("outbox relay running", "topics", []string{kafka.Topic, kafka.AnalyticsTopic})
			if err := relay.Run(relayCtx); err != nil && !errors.Is(err, context.Canceled) {
				serverErr <- err
			}
		}()
	} else {
		close(relayDone)
	}

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-stop:
		cancelCatalogueConsumer()
		if kafkaCatalogue != nil {
			_ = kafkaCatalogue.Close()
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := server.Shutdown(shutdownCtx)
		// Let the relay finish its in-flight pass so an event committed by
		// a request that completed just before shutdown is not stranded
		// until the next pod boots.
		stopRelay()
		select {
		case <-relayDone:
		case <-shutdownCtx.Done():
			logger.Warn("outbox relay did not stop before the shutdown deadline")
		}
		return err
	}
	return nil
}

// newEventPublisher selects an EventPublisher via EVENT_PUBLISHER
// (kafka|log, default log) so existing behavior — and existing tests — are
// unaffected unless kafka is explicitly opted into. It returns the
// publisher, the outbox relay to run alongside the HTTP server (nil when
// there is none), and a close func to release adapter resources on
// shutdown.
//
// In kafka mode the use cases publish into the transactional outbox
// (ADR 0016): both Kafka publishers act as Encoders feeding one
// OutboxPublisher, and the relay forwards each row to the topic it names.
// The direct MultiPublisher path is kept only for a nil pool, which this
// binary never has (DATABASE_URL is required) — it is what an in-memory
// composition would use, and it documents the matrix in the ADR.
func newEventPublisher(pool *pgxpool.Pool, shiftPlans ports.ShiftPlanRepo, logger *slog.Logger) (ports.EventPublisher, *postgres.OutboxRelay, func(), error) {
	switch envOrDefault("EVENT_PUBLISHER", "log") {
	case "kafka":
		brokers := strings.Split(envOrDefault("KAFKA_BROKERS", "localhost:9092"), ",")
		integration := kafka.NewPublisher(brokers, shiftPlans)
		// Fan-out: the same domain events also feed the analytics data product
		// on a SEPARATE topic (ADR-0010). The integration publisher/topic is
		// untouched; the analytics publisher is an additive second sink.
		analytics := kafka.NewAnalyticsPublisher(brokers, kafka.NewEventID)
		closeDirect := func() {
			if err := integration.Close(); err != nil {
				logger.Error("kafka publisher close failed", "error", err)
			}
			if err := analytics.Close(); err != nil {
				logger.Error("kafka analytics publisher close failed", "error", err)
			}
		}

		if pool == nil {
			logger.Info("event publisher configured", "publisher", "kafka", "mode", "direct",
				"brokers", brokers, "topic", kafka.Topic, "analytics_topic", kafka.AnalyticsTopic)
			return events.NewMultiPublisher(integration, analytics), nil, closeDirect, nil
		}

		sink := kafka.NewRelaySink(brokers)
		relay := postgres.NewOutboxRelay(pool, sink, logger,
			postgres.WithInterval(envDurationOrDefault("OUTBOX_RELAY_INTERVAL", time.Second)))
		logger.Info("event publisher configured", "publisher", "kafka", "mode", "outbox",
			"brokers", brokers, "topic", kafka.Topic, "analytics_topic", kafka.AnalyticsTopic)
		return postgres.NewOutboxPublisher(pool, integration, analytics), relay, func() {
			closeDirect()
			if err := sink.Close(); err != nil {
				logger.Error("kafka relay sink close failed", "error", err)
			}
		}, nil
	case "log":
		logger.Info("event publisher configured", "publisher", "log")
		return events.NewLogPublisher(logger), nil, func() {}, nil
	default:
		return nil, nil, nil, fmt.Errorf("unknown EVENT_PUBLISHER %q (want kafka or log)", os.Getenv("EVENT_PUBLISHER"))
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

// configureAuth builds the fleet-standard REST identity (ADR-0017 /
// warehouse-ops-agent ADR 0005): static bearer keys from API_READ_KEY /
// API_READWRITE_KEY (falling back to MCP_READ_KEY / MCP_READWRITE_KEY) and
// AUTH_MODE=enforce|log|off. The default mode is "enforce" when at least
// one key is configured and "off" (with a loud WARN) when none is, so local
// runs and tests without keys keep working. Key material is never logged.
func configureAuth(getenv func(string) string, logger *slog.Logger) (*auth.StaticKeyAuth, auth.Mode) {
	authn := auth.NewStaticKeyAuth(auth.KeysFromEnv(getenv))
	defaultMode := auth.ModeOff
	if authn.HasKeys() {
		defaultMode = auth.ModeEnforce
	}
	mode := auth.ParseMode(getenv("AUTH_MODE"), defaultMode)
	if mode == auth.ModeOff {
		logger.Warn("REST auth is OFF: no API_READ_KEY/API_READWRITE_KEY configured or AUTH_MODE=off")
	}
	logger.Info("REST auth configured", "mode", string(mode), "keys", len(auth.KeysFromEnv(getenv)))
	return authn, mode
}

// buildMeasuredRateClient selects a ports.MeasuredRateClient via mode
// (http|permissive), defaulting to "permissive" so unit tests, local dev,
// and CI never reach the network unless explicitly opted in -- the same
// pattern order-management uses for INVENTORY_STORAGE_MODE.
//
// apiKey (LABOR_PERFORMANCE_API_KEY) is the optional bearer sent on every
// call once labor-performance enforces REST auth (fleet ADR 0005).
func buildMeasuredRateClient(mode, baseURL, apiKey string, logger *slog.Logger) ports.MeasuredRateClient {
	if mode != "http" {
		logger.Info("labor-performance measured rate client configured", "mode", "permissive",
			"hint", "set LABOR_PERFORMANCE_MODE=http and LABOR_PERFORMANCE_BASE_URL for a real deployment")
		return laborperformance.NewPermissiveClient()
	}
	logger.Info("labor-performance measured rate client configured", "mode", "http", "base_url", baseURL, "bearer", apiKey != "")
	return laborperformance.NewClient(baseURL, nil, laborperformance.WithBearerToken(apiKey))
}

// buildInstalledCapacityClient selects a ports.InstalledCapacityClient via
// mode (http|permissive), defaulting to "permissive" -- unlike
// buildMeasuredRateClient's fail-open default, the permissive mode here
// makes CommitShiftPlan fail EVERY commit until INSTALLED_CAPACITY_MODE=http
// is explicitly set, since a shift-plan commit mutates real state and
// this fleet's own rule is to fail loud for anything that mutates real
// state. See ADR-0014.
//
// apiKey (FULFILLMENT_EXECUTION_API_KEY) is the optional bearer sent on
// every call once fulfillment-execution enforces REST auth (fleet ADR 0005).
func buildInstalledCapacityClient(mode, baseURL, apiKey string, logger *slog.Logger) ports.InstalledCapacityClient {
	if mode != "http" {
		logger.Warn("fulfillment-execution installed capacity client configured", "mode", "permissive",
			"hint", "every ShiftPlan commit will fail until INSTALLED_CAPACITY_MODE=http and FULFILLMENT_EXECUTION_BASE_URL are set for a real deployment")
		return fulfillmentexecution.NewPermissiveClient()
	}
	logger.Info("fulfillment-execution installed capacity client configured", "mode", "http", "base_url", baseURL, "bearer", apiKey != "")
	return fulfillmentexecution.NewClient(baseURL, nil, fulfillmentexecution.WithBearerToken(apiKey))
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

// envDurationOrDefault parses key as a time.Duration, falling back on
// absence or a malformed/non-positive value: the relay interval is a tuning
// knob, not a contract, so it never fails the boot.
func envDurationOrDefault(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		slog.Warn("invalid duration env var, using default", "key", key, "value", v, "default", def)
		return def
	}
	return d
}
