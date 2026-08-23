---
id: 0005-rfc-7807-problem-details
title: 0005. RFC 7807 Problem Details for every error response
sidebar_label: 0005. RFC 7807 errors
sidebar_position: 6
description: Replacing the bespoke error shape with a standard, machine-readable problem document.
---

# 0005. RFC 7807 Problem Details for every error response

## Status

Accepted. Migrated in commit `c02ab1a` ("Task 11: REST API hardening, RFC 7807
errors, OpenAPI 3.0.3 docs, Spectral CI gate").

## Context

The service originally returned a bespoke error shape:

```json
{"error": "associate lacks the certification required for this path"}
```

This service produces a lot of semantically distinct failures — eight distinct
`409` invariant rejections alone, plus seven `400` validation cases. A client
integrating against it needs to tell them apart. With the bespoke shape the only
discriminator is the HTTP status plus a free-text English string, which means
either treating every `409` identically or **string-matching on prose** — a
contract that breaks the first time someone improves the wording.

The API audit that produced this decision also found the shape inconsistent with
the rest of the platform, and untypeable in OpenAPI beyond "an object with a
string field," which made the generated documentation useless for error
handling.

RFC 7807 (`application/problem+json`) is the IETF standard for exactly this: a
`type` URI identifying the error *category*, a fixed `title`, the `status`, a
per-occurrence `detail`, and an `instance` identifying the specific request.

## Decision

**Every error response from this service is an RFC 7807 problem document**,
served as `application/problem+json`.

```json
{
  "type": "https://errors.workforce-management.warehouse-systems.dev/certification-required",
  "title": "Associate lacks the certification required for this path",
  "status": 409,
  "detail": "associate lacks the certification required for this path",
  "instance": "/associates/assoc-1/assignments"
}
```

The design rules:

- **`type` identifies the category, not the occurrence.** One fixed URI per
  sentinel domain error, derived from a `problemCategory{slug, title}` mapping
  in `internal/adapters/inbound/http/errors.go`.
- **`type` does not need to resolve.** RFC 7807 explicitly permits a
  non-dereferenceable URI; it is an identifier. The base is
  `https://errors.workforce-management.warehouse-systems.dev`, which is not a
  live host and is not intended to become one.
- **`title` is fixed per category; `detail` varies per occurrence.** `detail`
  carries the underlying `err.Error()` text, preserving the information the old
  shape carried while making it the *non*-contractual part.
- **`instance` is the request path.**
- **There is no route through the adapter that emits anything else.** Errors
  matching no category fall back to a status-keyed generic one —
  `malformed-request-body` for `400`, `internal-error` otherwise — so a
  well-formed problem document is guaranteed on every failure path.

The mapping from typed domain error to status stays in the adapter. The domain
returns sentinel errors and knows nothing about HTTP.

## Consequences

**Easier**

- Clients switch on `type` — a stable, machine-readable identifier — and display
  `detail`. Error-message wording can be improved without breaking anyone.
- The eight distinct `409` invariant rejections are individually
  distinguishable, so a UI can respond specifically to "on break" versus
  "missing certification."
- One `Problem` schema in `apis/openapi.yaml`, referenced by every error
  response on every operation, and Spectral-linted in CI.
- Consistent with the rest of the `warehouse-systems` platform — the newest
  service, `facility-layout`, was specified to go straight to RFC 7807 without
  ever shipping a bespoke shape.

**Harder**

- **A breaking change for any existing client** reading `.error`. Accepted
  deliberately: the migration happened before there were external consumers, and
  waiting would have made it strictly more expensive.
- Every new sentinel domain error now needs a `problemCategory` entry as well as
  a status mapping — two places instead of one. The fallback stops that from
  being a correctness problem, but a forgotten entry degrades to a generic
  category.
- Slightly more verbose responses. Irrelevant at this volume.

**Now true**

- Every status mapping is asserted by an `httptest` test against the real chi
  router, and the `409` paths are additionally covered by the godog acceptance
  specs.
- The full catalogue of problem types is documented on the
  [Errors](../api-reference/errors.md) page and generated into the
  [REST API reference](../api-reference/rest/workforce-management-api.info.mdx)
  from the spec.
