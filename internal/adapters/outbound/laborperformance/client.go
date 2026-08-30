// Package laborperformance provides outbound MeasuredRateClient
// implementations: an HTTP client against labor-performance's published
// GetTaskTypePerformance contract, and a permissive no-op selected by
// LABOR_PERFORMANCE_MODE (default "permissive") so unit tests and CI never
// reach the network.
//
// This follows the env-selected adapter pattern already used elsewhere in
// this fleet (order-management -> inventory-storage, wes-work-planning ->
// inventory-storage, fulfillment-execution -> inventory-storage), with the
// SAME fail-open choice order-management's own README documents for a soft,
// optional enrichment: unlike order-management's inventory-storage client
// (which fails LOUD because reserving real stock is not optional), a
// measured rate is a soft input to ProposePathPlan -- the use case already
// accepts a caller-supplied plannedRate override, so a downstream miss here
// must never block a plan proposal. See ADR-000X in this repo's docs for the
// full rationale.
package laborperformance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/claudioed/workforce-management/internal/application/ports"
	"github.com/claudioed/workforce-management/internal/domain/shared"
)

// DefaultTimeout bounds a single call to labor-performance, so a slow or
// hanging Supplier does not stall ProposePathPlan indefinitely.
const DefaultTimeout = 3 * time.Second

// HTTPDoer is the subset of *http.Client this adapter depends on, so unit
// tests can substitute a fake transport without a real server.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client is a plain net/http implementation of ports.MeasuredRateClient,
// calling labor-performance's published contract:
//
//	GET /task-types/{taskType} -> 200 with TaskTypePerformance
//
// It imports nothing from the labor-performance module: the response shape
// below is a local mirror of that service's published wire contract (see
// its apis/openapi.yaml, TaskTypePerformance schema).
type Client struct {
	baseURL string
	doer    HTTPDoer
}

// NewClient builds a Client against baseURL (from LABOR_PERFORMANCE_BASE_URL).
// A nil doer defaults to an *http.Client with DefaultTimeout.
func NewClient(baseURL string, doer HTTPDoer) *Client {
	if doer == nil {
		doer = &http.Client{Timeout: DefaultTimeout}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), doer: doer}
}

// taskTypePerformanceResponse mirrors labor-performance's
// TaskTypePerformance response schema. Only meanActualSeconds is consumed;
// taskCount/meanEfficiencyPct are decoded but unused today, kept for a
// future caller.
type taskTypePerformanceResponse struct {
	TaskType          string   `json:"taskType"`
	TaskCount         int      `json:"taskCount"`
	MeanEfficiencyPct *float64 `json:"meanEfficiencyPct"`
	MeanActualSeconds *float64 `json:"meanActualSeconds"`
}

// taskTypeForPathId maps this context's lowercase PathId (e.g. "pack",
// "pick") onto labor-performance's uppercase TaskType enum (PICK, PACK,
// SLAM -- see fulfillment-execution's CLAUDE.md "Process path" entry,
// the shared source of that enum). Not every PathId has a TaskType
// counterpart (e.g. "stow", "hazmat" are workforce-management-only
// paths with no measurable task type in labor-performance) -- those
// report ok=false so the caller can fail fast without an HTTP round
// trip.
func taskTypeForPathId(pathId shared.PathId) (taskType string, ok bool) {
	switch strings.ToUpper(string(pathId)) {
	case "PICK", "PACK", "SLAM":
		return strings.ToUpper(string(pathId)), true
	default:
		return "", false
	}
}

// MeanActualSeconds calls GET /task-types/{taskType}/performance on
// labor-performance, mapping this context's PathId onto
// labor-performance's TaskType via taskTypeForPathId. Returns
// ports.ErrMeasuredRateUnavailable (never a raw transport/decode error) on
// ANY failure -- unreachable service, non-200, malformed body, a PathId
// with no TaskType counterpart, or a real 200 with a null
// meanActualSeconds (no measurable data exists yet) -- so
// ProposePathPlan's single error-handling branch never needs to
// distinguish those cases; all of them mean the same thing to a caller:
// fall back to a caller-supplied rate.
func (c *Client) MeanActualSeconds(ctx context.Context, pathId shared.PathId) (float64, error) {
	taskType, ok := taskTypeForPathId(pathId)
	if !ok {
		return 0, fmt.Errorf("%w: path %q has no labor-performance task type", ports.ErrMeasuredRateUnavailable, pathId)
	}
	endpoint := fmt.Sprintf("%s/task-types/%s/performance", c.baseURL, url.PathEscape(taskType))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ports.ErrMeasuredRateUnavailable, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.doer.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ports.ErrMeasuredRateUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("%w: unexpected status %d", ports.ErrMeasuredRateUnavailable, resp.StatusCode)
	}

	var decoded taskTypePerformanceResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return 0, fmt.Errorf("%w: %w", ports.ErrMeasuredRateUnavailable, err)
	}
	if decoded.MeanActualSeconds == nil {
		return 0, fmt.Errorf("%w: no measurable data recorded for this task type yet", ports.ErrMeasuredRateUnavailable)
	}
	return *decoded.MeanActualSeconds, nil
}
