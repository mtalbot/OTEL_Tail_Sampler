Feature: Observability scenarios for OTEL Tail Sampler

  Scenario: Successful multi-span trace processing
    Given a multi-span trace with 3 spans
    When the trace is sent to the sampler
    Then all spans should be processed and exported successfully
    And metrics should be generated for the trace

  Scenario: Failed request in a multi-span trace
    Given a multi-span trace with one failed span
    When the trace is sent to the sampler
    Then the failed span should be properly handled
    And error metrics should be generated
    And the trace should still be exported

  Scenario: Slow request in a multi-span trace
    Given a multi-span trace with one slow span
    When the trace is sent to the sampler
    Then the slow span should trigger latency-based sampling
    And appropriate metrics should be generated

  Scenario: Mixed success and failure in multi-span trace
    Given a multi-span trace with mixed success and failure spans
    When the trace is sent to the sampler
    Then all spans should be processed appropriately
    And error and latency metrics should be generated correctly

  Scenario: Error propagation through system
    Given an error occurs during trace processing
    When the error is encountered
    Then error should be logged properly
    And the system should continue processing other traces