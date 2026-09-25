import { describe, expect, it } from "vitest";
import { resolveWorkforceApiBase } from "./config";

describe("resolveWorkforceApiBase", () => {
  it("builds the production API base from the runtime API origin", () => {
    expect(resolveWorkforceApiBase({ apiOrigin: "http://localhost:8000" }, true)).toBe(
      "http://localhost:8000/api/workforce-management",
    );
  });

  it("normalizes a trailing slash on the runtime API origin", () => {
    expect(resolveWorkforceApiBase({ apiOrigin: "https://warehouse.example/" }, true)).toBe(
      "https://warehouse.example/api/workforce-management",
    );
  });

  it("fails loudly when production runtime configuration has no API origin", () => {
    expect(() => resolveWorkforceApiBase({}, true)).toThrow(
      "window.__WAREHOUSE_CONFIG__.apiOrigin is required in production",
    );
  });

  it("retains the existing standalone API origin in Vite development", () => {
    expect(resolveWorkforceApiBase({}, false)).toBe("http://localhost:8085");
  });
});
