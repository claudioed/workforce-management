import { useState, type FormEvent } from "react";
import { WORKFORCE_API_BASE } from "../config";
import type { StaffingGap } from "../types";
import { Card, StatusPill, useFetch } from "@warehouse/ui-kit";

/**
 * Staffing-gap-by-path dashboard. workforce-management's REST API only
 * exposes GET /paths/{pathId}/staffing-gap?buildingId=&shiftId= (ShiftPlan
 * is keyed by building+shift, so both query params are required alongside
 * the path) -- see router.go's staffingGap handler. This screen is scoped
 * to what actually exists today: look up one path's gap for one
 * building/shift. A fleet-wide "all paths at once" view needs a new
 * list-style endpoint on workforce-management first -- same category of
 * gap as order-mgmt-mfe's missing GET /orders?status= (a fast-follow, not
 * blocking this pilot).
 */
export function WorkforceScreen() {
  const [pathIdInput, setPathIdInput] = useState("");
  const [buildingIdInput, setBuildingIdInput] = useState("wh1");
  const [shiftIdInput, setShiftIdInput] = useState("shift-1");
  const [query, setQuery] = useState<{ pathId: string; buildingId: string; shiftId: string } | null>(null);

  const url = query
    ? `${WORKFORCE_API_BASE}/paths/${encodeURIComponent(query.pathId)}/staffing-gap?buildingId=${encodeURIComponent(
        query.buildingId,
      )}&shiftId=${encodeURIComponent(query.shiftId)}`
    : null;
  const { data, loading, error } = useFetch<StaffingGap>(url);

  function onSubmit(e: FormEvent) {
    e.preventDefault();
    const pathId = pathIdInput.trim();
    const buildingId = buildingIdInput.trim();
    const shiftId = shiftIdInput.trim();
    if (pathId && buildingId && shiftId) {
      setQuery({ pathId, buildingId, shiftId });
    }
  }

  const gapStatus = data ? (data.understaffed ? "Understaffed" : "Staffed") : null;
  const maxHeads = data ? Math.max(data.plannedHeads, data.activeHeads, 1) : 1;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "var(--wh-space-5)" }}>
      <div>
        <h1 style={{ fontSize: "var(--wh-font-size-2xl)", margin: 0 }}>Workforce</h1>
        <p style={{ color: "var(--wh-color-text-muted)", marginTop: 4 }}>
          workforce-management · staffing gap by path -- planned vs active headcount
        </p>
      </div>

      <form onSubmit={onSubmit} style={{ display: "flex", gap: "var(--wh-space-2)", flexWrap: "wrap" }}>
        <input
          value={pathIdInput}
          onChange={(e) => setPathIdInput(e.target.value)}
          placeholder="Path ID"
          style={inputStyle({ flex: 1, minWidth: 160 })}
        />
        <input
          value={buildingIdInput}
          onChange={(e) => setBuildingIdInput(e.target.value)}
          placeholder="Building ID"
          style={inputStyle({ width: 140 })}
        />
        <input
          value={shiftIdInput}
          onChange={(e) => setShiftIdInput(e.target.value)}
          placeholder="Shift ID"
          style={inputStyle({ width: 140 })}
        />
        <button type="submit" style={buttonStyle}>
          Check gap
        </button>
      </form>

      {!query && (
        <Card>
          <div style={{ color: "var(--wh-color-text-muted)" }}>
            Enter a path, building, and shift to see planned vs active headcount.
          </div>
        </Card>
      )}

      {error && (
        <Card>
          <div style={{ color: "var(--wh-color-status-danger)" }}>{error.message}</div>
        </Card>
      )}

      {loading && query && (
        <Card title={query.pathId}>
          <div style={{ color: "var(--wh-color-text-muted)" }}>Loading staffing gap…</div>
        </Card>
      )}

      {data && !loading && gapStatus && (
        <Card
          title={data.pathId}
          actions={<StatusPill status={gapStatus} tone={data.understaffed ? "warning" : "success"} />}
        >
          <div
            style={{
              display: "flex",
              gap: "var(--wh-space-6)",
              marginBottom: "var(--wh-space-5)",
              fontSize: "var(--wh-font-size-sm)",
              color: "var(--wh-color-text-muted)",
            }}
          >
            <span>Building: {query?.buildingId}</span>
            <span>Shift: {query?.shiftId}</span>
          </div>

          <div style={{ display: "flex", flexDirection: "column", gap: "var(--wh-space-4)" }}>
            <HeadcountBar label="Planned heads" value={data.plannedHeads} max={maxHeads} tone="neutral" />
            <HeadcountBar
              label="Active heads"
              value={data.activeHeads}
              max={maxHeads}
              tone={data.understaffed ? "warning" : "success"}
            />
          </div>
        </Card>
      )}
    </div>
  );
}

/** Simple two-number visual comparison (planned vs active) using inline
 *  styled divs matching the design tokens -- deliberately not a new
 *  ui-kit chart component for this single pilot dashboard; revisit if a
 *  second screen needs the same shape. */
function HeadcountBar({
  label,
  value,
  max,
  tone,
}: {
  label: string;
  value: number;
  max: number;
  tone: "neutral" | "success" | "warning";
}) {
  const pct = max > 0 ? Math.min(100, (value / max) * 100) : 0;
  const fg =
    tone === "success"
      ? "var(--wh-color-status-success)"
      : tone === "warning"
        ? "var(--wh-color-status-warning)"
        : "var(--wh-color-text-muted)";
  const bg =
    tone === "success"
      ? "var(--wh-color-status-success-bg)"
      : tone === "warning"
        ? "var(--wh-color-status-warning-bg)"
        : "var(--wh-color-bg-sunken)";

  return (
    <div>
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          fontSize: "var(--wh-font-size-sm)",
          marginBottom: 6,
        }}
      >
        <span>{label}</span>
        <span style={{ fontWeight: 600, fontFamily: "var(--wh-font-mono)" }}>{value}</span>
      </div>
      <div
        style={{
          height: 10,
          borderRadius: "var(--wh-radius-pill)",
          background: bg,
          overflow: "hidden",
        }}
      >
        <div
          style={{
            height: "100%",
            width: `${pct}%`,
            background: fg,
            borderRadius: "var(--wh-radius-pill)",
            transition: "width 200ms ease",
          }}
        />
      </div>
    </div>
  );
}

function inputStyle(extra: Record<string, string | number>) {
  return {
    padding: "10px 12px",
    borderRadius: "var(--wh-radius-md)",
    border: "1px solid var(--wh-color-border)",
    background: "var(--wh-color-bg-sunken)",
    color: "var(--wh-color-text)",
    fontFamily: "var(--wh-font-mono)",
    fontSize: "var(--wh-font-size-sm)",
    ...extra,
  } as const;
}

const buttonStyle = {
  padding: "10px 18px",
  borderRadius: "var(--wh-radius-md)",
  border: "none",
  background: "var(--wh-color-accent)",
  color: "#fff",
  fontWeight: 600,
  fontSize: "var(--wh-font-size-sm)",
  cursor: "pointer",
} as const;
