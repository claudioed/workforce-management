Feature: Assigning labor to a path
  As a shift manager
  I want to record which associate is working which path
  So that the labor picture is legible without ever linking an associate to a task

  A LaborAssignment is one associate on one path for an interval. It requires
  the Certification the path demands, and exactly one assignment per associate
  is ACTIVE at any moment.

  @bdd
  Scenario: Assigning a certified associate to a path succeeds
    Given an AssociateShift is started for associate "assoc-1" with no certifications
    And associate "assoc-1" is certified for "pack"
    When associate "assoc-1" is assigned to path "pack"
    Then the LaborAssignment is created with active path "pack"

  @bdd
  Scenario: Assigning an uncertified associate to a path is rejected
    Given an AssociateShift is started for associate "assoc-2" with certifications "pick"
    When associate "assoc-2" is assigned to path "pack"
    Then the assignment is rejected with status 409 and problem type "certification-required"

  @bdd
  Scenario: An associate cannot hold two active assignments at once (no double-booking)
    Given an AssociateShift is started for associate "assoc-3" with certifications "pick,pack"
    And a ShiftPlan is committed for building "bldg-1" shift "shift-1" with lines:
      | pathId | plannedHeads | plannedRate | plannedHours | installedStations |
      | pick   | 1            | 30          | 8            | 5                 |
      | pack   | 1            | 30          | 8            | 5                 |
    When associate "assoc-3" is assigned to path "pick"
    And associate "assoc-3" is assigned to path "pack"
    Then the LaborAssignment for associate "assoc-3" has exactly one ACTIVE assignment, on path "pack"
    And path "pick" has 0 active heads in building "bldg-1" shift "shift-1"
    And path "pack" has 1 active head in building "bldg-1" shift "shift-1"

  @bdd
  Scenario: Assigning an associate who is on an active break is rejected
    Given an AssociateShift is started for associate "assoc-4" with certifications "pack"
    And associate "assoc-4" has started a break
    When associate "assoc-4" is assigned to path "pack"
    Then the assignment is rejected with status 409 and problem type "associate-on-break"
