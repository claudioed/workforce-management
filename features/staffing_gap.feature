Feature: Surfacing the staffing gap for a path
  As a shift manager
  I want to see plannedHeads against active LaborAssignments for a path
  So that an understaffed path is visible

  PathUnderstaffed is a flag, not a decision: this context surfaces the gap and
  never moves anyone. Moving people stays a human call.

  @bdd
  Scenario: An understaffed path is flagged when active assignments are below plan
    Given an AssociateShift is started for associate "assoc-1" with certifications "pack"
    And a ShiftPlan is committed for building "bldg-1" shift "shift-1" with lines:
      | pathId | plannedHeads | plannedRate | plannedHours | installedStations |
      | pack   | 3            | 30          | 24           | 10                |
    And associate "assoc-1" is assigned to path "pack"
    When the staffing gap for path "pack" is requested for building "bldg-1" shift "shift-1"
    Then path "pack" is flagged PathUnderstaffed with 3 planned heads and 1 active head
