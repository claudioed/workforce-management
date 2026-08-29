/** Local-dev base URL for workforce-management's own REST API. Mirrors
 *  e2e-tests/env.sh's WORKFORCE_HTTP_PORT=8085. See warehouse-console's
 *  src/config.ts for the note on swapping to runtime config before
 *  multi-environment deployment. */
export const WORKFORCE_API_BASE = "http://localhost:8085";
