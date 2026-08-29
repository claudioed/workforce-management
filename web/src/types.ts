/** Wire types mirroring workforce-management's dto.go response shapes
 *  (staffingGapResponse) exactly -- kept hand-in-sync with the Go DTOs
 *  rather than code-generated for v1; revisit with openapi-typescript once
 *  the OpenAPI spec is the enforced source of truth. */
export interface StaffingGap {
  pathId: string;
  plannedHeads: number;
  activeHeads: number;
  understaffed: boolean;
}
