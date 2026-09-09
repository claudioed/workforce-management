package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/claudioed/workforce-management/internal/adapters/inbound/auth"
)

const (
	testReadKey      = "test-read-key"
	testReadWriteKey = "test-readwrite-key"
)

func newAuthedRouter(mode auth.Mode) http.Handler {
	authn := auth.NewStaticKeyAuth(map[string]auth.Scope{
		testReadKey:      auth.ScopeRead,
		testReadWriteKey: auth.ScopeReadWrite,
	})
	return NewRouter(newTestHandler(), testLogger, "", WithAuth(authn, mode))
}

func doAuthed(t *testing.T, r http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func assertProblem(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantSlug string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, wantStatus, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	var p problemDetails
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if p.Type != problemErrorsURIBase+"/"+wantSlug {
		t.Fatalf("type = %q, want %q", p.Type, problemErrorsURIBase+"/"+wantSlug)
	}
	if p.Status != wantStatus {
		t.Fatalf("problem.status = %d, want %d", p.Status, wantStatus)
	}
}

// The fleet router table (rest-auth rollout brief, step 7): one GET and one
// mutating route through every credential class, plus /healthz open.
func TestRouterAuth_EnforceTable(t *testing.T) {
	router := newAuthedRouter(auth.ModeEnforce)
	const getPath = "/paths/pack/staffing-gap?buildingId=B1&shiftId=S1"
	const postPath = "/associates/a1/start-shift"
	const postBody = `{"certifications":["pack"]}`

	t.Run("no token GET -> 401 problem+json with WWW-Authenticate", func(t *testing.T) {
		rec := doAuthed(t, router, http.MethodGet, getPath, "", "")
		assertProblem(t, rec, http.StatusUnauthorized, "unauthenticated")
		if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Bearer ") {
			t.Fatalf("WWW-Authenticate = %q, want a Bearer challenge", got)
		}
	})
	t.Run("no token POST -> 401", func(t *testing.T) {
		rec := doAuthed(t, router, http.MethodPost, postPath, postBody, "")
		assertProblem(t, rec, http.StatusUnauthorized, "unauthenticated")
	})
	t.Run("unknown token GET -> 401", func(t *testing.T) {
		rec := doAuthed(t, router, http.MethodGet, getPath, "", "nope")
		assertProblem(t, rec, http.StatusUnauthorized, "unauthenticated")
	})
	t.Run("read key GET -> 2xx/4xx from the handler, never 401/403", func(t *testing.T) {
		rec := doAuthed(t, router, http.MethodGet, getPath, "", testReadKey)
		// No shift plan is seeded, so the handler answers 404 — what matters
		// here is that the request passed the auth gate and reached it.
		if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
			t.Fatalf("read key on GET: got %d, want the handler to answer", rec.Code)
		}
		if rec.Code != http.StatusNotFound {
			t.Fatalf("read key on GET: got %d, want 404 from the handler (empty repo)", rec.Code)
		}
	})
	t.Run("read key POST -> 403 insufficient-scope", func(t *testing.T) {
		rec := doAuthed(t, router, http.MethodPost, postPath, postBody, testReadKey)
		assertProblem(t, rec, http.StatusForbidden, "insufficient-scope")
		if rec.Header().Get("WWW-Authenticate") != "" {
			t.Fatal("403 must not carry a WWW-Authenticate challenge (the credential was valid)")
		}
	})
	t.Run("rw key POST -> 2xx", func(t *testing.T) {
		rec := doAuthed(t, router, http.MethodPost, postPath, postBody, testReadWriteKey)
		if rec.Code != http.StatusCreated {
			t.Fatalf("rw key on POST: got %d, want 201 (body %s)", rec.Code, rec.Body.String())
		}
	})
	t.Run("rw key GET -> handler answers", func(t *testing.T) {
		rec := doAuthed(t, router, http.MethodGet, getPath, "", testReadWriteKey)
		if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
			t.Fatalf("rw key on GET: got %d, want the handler to answer", rec.Code)
		}
	})
	t.Run("/healthz with no token -> 200", func(t *testing.T) {
		rec := doAuthed(t, router, http.MethodGet, "/healthz", "", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /healthz without a token: got %d, want 200", rec.Code)
		}
		if rec.Header().Get("WWW-Authenticate") != "" {
			t.Fatal("/healthz must sit outside the auth middleware")
		}
	})
}

// AUTH_MODE=log lets everything through (the rollout gate) — a request with
// no credential still reaches the handler.
func TestRouterAuth_LogModeLetsRequestsThrough(t *testing.T) {
	router := newAuthedRouter(auth.ModeLog)
	rec := doAuthed(t, router, http.MethodPost, "/associates/a2/start-shift", `{"certifications":["pack"]}`, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("log mode without a token: got %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	rec = doAuthed(t, router, http.MethodPost, "/associates/a3/start-shift", `{"certifications":["pack"]}`, testReadKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("log mode with an under-scoped key: got %d, want 201", rec.Code)
	}
}

// AUTH_MODE=off (and the option omitted entirely) leaves the router open so
// existing handler tests keep running without keys.
func TestRouterAuth_OffModeIsANoop(t *testing.T) {
	for name, router := range map[string]http.Handler{
		"explicit off":  newAuthedRouter(auth.ModeOff),
		"no option":     NewRouter(newTestHandler(), testLogger, ""),
		"nil authn":     NewRouter(newTestHandler(), testLogger, "", WithAuth(nil, auth.ModeEnforce)),
		"empty mode":    NewRouter(newTestHandler(), testLogger, "", WithAuth(auth.NewStaticKeyAuth(nil), "")),
		"keyless authn": NewRouter(newTestHandler(), testLogger, "", WithAuth(auth.NewStaticKeyAuth(nil), auth.ModeOff)),
	} {
		t.Run(name, func(t *testing.T) {
			rec := doAuthed(t, router, http.MethodPost, "/associates/a4/start-shift", `{"certifications":["pack"]}`, "")
			if rec.Code != http.StatusCreated {
				t.Fatalf("got %d, want 201 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

// Enforce mode with an authenticator that holds NO keys fails closed: every
// business route is 401, /healthz still 200.
func TestRouterAuth_EnforceWithoutKeysFailsClosed(t *testing.T) {
	router := NewRouter(newTestHandler(), testLogger, "", WithAuth(auth.NewStaticKeyAuth(nil), auth.ModeEnforce))
	rec := doAuthed(t, router, http.MethodPost, "/associates/a5/start-shift", `{"certifications":["pack"]}`, "anything")
	assertProblem(t, rec, http.StatusUnauthorized, "unauthenticated")
	rec = doAuthed(t, router, http.MethodGet, "/healthz", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz: got %d, want 200", rec.Code)
	}
}
