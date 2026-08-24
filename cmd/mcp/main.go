// Command mcp is the composition root for the Workforce Management MCP
// server: it wires env config to outbound adapters, adapters to the use
// cases, and those to the inbound MCP adapter, then serves MCP over Streamable
// HTTP. It is a second, independent deployable alongside cmd/workforce (the
// HTTP service), per ADR-0008.
//
// Auth is a static bearer key (no IdP): set MCP_READ_KEY (and optionally
// MCP_READWRITE_KEY) from a Kubernetes Secret. A request must present a valid
// key; the scope it grants gates the tools.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	inboundmcp "github.com/claudioed/workforce-management/internal/adapters/inbound/mcp"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/clock"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/events"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/memory"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/postgres"
	"github.com/claudioed/workforce-management/internal/adapters/outbound/telemetry"
	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/application/usecases"
)

func main() {
	if err := run(); err != nil {
		slog.Error("mcp server exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	logger := newLogger(envOrDefault("LOG_LEVEL", "info"))
	slog.SetDefault(logger)

	// Same non-blocking telemetry setup as the HTTP service: an unreachable
	// Collector degrades to dropped telemetry, never a server that won't start.
	ctx := context.Background()
	serviceName := envOrDefault("OTEL_SERVICE_NAME", "workforce-management-mcp")
	otlpEndpoint := envOrDefault("OTEL_EXPORTER_OTLP_ENDPOINT", telemetry.DefaultOTLPEndpoint)
	shutdownTelemetry, err := telemetry.Setup(ctx, serviceName, serviceVersion(), otlpEndpoint)
	if err != nil {
		logger.Error("opentelemetry setup degraded", "error", err)
	}
	if shutdownTelemetry != nil {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := shutdownTelemetry(shutdownCtx); err != nil {
				logger.Warn("telemetry shutdown did not flush cleanly", "error", err)
			}
		}()
	}

	httpAddr := envOrDefault("MCP_ADDR", ":8090")
	databaseURL := os.Getenv("DATABASE_URL")
	migrationsPath := envOrDefault("MIGRATIONS_PATH", "migrations")
	maxHoursPerShift := envFloatOrDefault("MAX_HOURS_PER_SHIFT", 8.0)

	// Select in-memory vs Postgres repos the same way the platform does: no
	// DATABASE_URL means local/in-memory adapters; a URL means migrate then
	// connect a pgx pool.
	var (
		associates  ports.AssociateRepo
		shiftPlans  ports.ShiftPlanRepo
		assignments ports.AssignmentRepo
	)
	if databaseURL == "" {
		logger.Info("database url not configured; using in-memory adapters")
		associates = memory.NewAssociateRepo()
		shiftPlans = memory.NewShiftPlanRepo()
		assignments = memory.NewAssignmentRepo()
	} else {
		if err := postgres.Migrate(databaseURL, migrationsPath); err != nil {
			return err
		}
		pool, err := postgres.NewPool(ctx, databaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()
		associates = postgres.NewAssociateRepo(pool)
		shiftPlans = postgres.NewShiftPlanRepo(pool)
		assignments = postgres.NewAssignmentRepo(pool)
	}

	sysClock := clock.System{}

	// The MCP adapter reuses the SAME use cases the HTTP adapter uses:
	// GetStaffingGap and ProposePathPlan (read) and AssignLabor (write).
	// AssignLabor needs a publisher and clock; the MCP server is not the
	// platform's primary event publisher (cmd/workforce is), so it logs the
	// events it raises rather than publishing to Kafka.
	publisher := events.NewLogPublisher(logger)
	deps := inboundmcp.Deps{
		GetStaffingGap:  &usecases.GetStaffingGap{ShiftPlans: shiftPlans, Assignments: assignments, Events: publisher, Clock: sysClock},
		ProposePathPlan: &usecases.ProposePathPlan{Events: publisher, Clock: sysClock},
		AssignLabor:     &usecases.AssignLabor{Associates: associates, Assignments: assignments, Events: publisher, Clock: sysClock, MaxHoursPerShift: maxHoursPerShift},
	}
	server := inboundmcp.NewServer(deps)

	auth := inboundmcp.NewStaticKeyAuth(authKeys(logger))
	handler := inboundmcp.Handler(server, auth)

	srv := &http.Server{Addr: httpAddr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("mcp server listening (Streamable HTTP)", "addr", httpAddr)
		serverErr <- srv.ListenAndServe()
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
		return srv.Shutdown(shutdownCtx)
	}
	return nil
}

// authKeys reads the bearer keys from the environment. MCP_READ_KEY grants
// read scope; MCP_READWRITE_KEY grants read-write. If neither is set the server
// still starts but rejects every request (fail closed) — a missing key must
// never mean "open to everyone". The keys themselves are never logged.
func authKeys(logger *slog.Logger) map[string]inboundmcp.Scope {
	keys := make(map[string]inboundmcp.Scope)
	if k := os.Getenv("MCP_READ_KEY"); k != "" {
		keys[k] = inboundmcp.ScopeRead
	}
	if k := os.Getenv("MCP_READWRITE_KEY"); k != "" {
		keys[k] = inboundmcp.ScopeReadWrite
	}
	if len(keys) == 0 {
		logger.Warn("no MCP_READ_KEY or MCP_READWRITE_KEY set; server will reject all requests")
	}
	return keys
}

// version is the service version reported as the OTel service.version
// resource attribute, overridable at build time (-ldflags "-X main.version=...")
// and otherwise falling back to the SERVICE_VERSION env var, then "dev".
var version = ""

func serviceVersion() string {
	if version != "" {
		return version
	}
	return envOrDefault("SERVICE_VERSION", "dev")
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(telemetry.NewTraceHandler(
		slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}),
	))
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
