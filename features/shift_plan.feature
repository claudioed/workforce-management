Feature: Committing a ShiftPlan
  As a shift manager
  I want to commit the split of headcount across paths for a shift
  So that the building has one agreed ShiftPlan for the shift

  The ShiftPlan is committed by a human: the software proposes heads, a human
  commits them. Committing independently re-validates the invariant that a
  path's plannedHeads never exceeds its installed stations.

  @bdd
  Scenario: Committing a ShiftPlan within installed-station capacity succeeds
    When committing a ShiftPlan for building "bldg-1" shift "shift-1" with lines:
      | pathId | plannedHeads | plannedRate | plannedHours | installedStations |
      | pack   | 6            | 30          | 40           | 10                |
    Then the ShiftPlan commit succeeds with 6 planned heads on path "pack"

  @bdd
  Scenario: Committing a ShiftPlan that exceeds installed stations is rejected
    When committing a ShiftPlan for building "bldg-1" shift "shift-1" with lines:
      | pathId | plannedHeads | plannedRate | plannedHours | installedStations |
      | pack   | 6            | 30          | 24           | 4                 |
    Then the ShiftPlan commit is rejected with status 409 and problem type "planned-heads-exceed-installed"
