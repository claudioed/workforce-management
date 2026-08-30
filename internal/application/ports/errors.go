package ports

import "errors"

// ErrNotFound is returned by a repository when the requested aggregate does
// not exist.
var ErrNotFound = errors.New("not found")

// ErrMeasuredRateUnavailable is returned by a MeasuredRateClient
// implementation for every failure mode a caller cannot usefully act on
// differently: an unreachable labor-performance, a malformed response, or
// a real 200 response reporting no measurable data yet for this path.
// ProposePathPlan's single error-handling branch depends on this being
// the ONLY error a MeasuredRateClient ever returns.
var ErrMeasuredRateUnavailable = errors.New("measured rate unavailable")
