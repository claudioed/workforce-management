Feature: Logging associate breaks
  As a shift manager
  I want breaks recorded against an AssociateShift
  So that an associate on a logged break is not put on a path

  @bdd
  Scenario: Starting and ending a break updates the associate's shift state
    Given an AssociateShift is started for associate "assoc-1" with certifications "pack"
    When associate "assoc-1" starts a break
    Then the last request succeeds with status 204
    When associate "assoc-1" is assigned to path "pack"
    Then the assignment is rejected with status 409 and problem type "associate-on-break"
    When associate "assoc-1" ends the break
    Then the last request succeeds with status 204
    When associate "assoc-1" is assigned to path "pack"
    Then the LaborAssignment is created with active path "pack"
