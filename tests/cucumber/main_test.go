package cucumber

import (
	"testing"

	"github.com/cucumber/godog"
)

func TestFeatures(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Simple approach - just run Godog without specifying paths
	status := godog.TestSuite{
		Name:                "observability",
		ScenarioInitializer: InitializeScenario,
		Options: &godog.Options{
			Format:   "pretty",
			TestingT: t, // Testing instance
		},
	}.Run()

	if status != 0 {
		t.Fatalf("Cucumber tests failed with status %d", status)
	}
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	tc := &testContext{}

	ctx.BeforeScenario(func(sc *godog.Scenario) {
		// Setup for each scenario
	})

	ctx.AfterScenario(func(sc *godog.Scenario, err error) {
		// Cleanup after each scenario
	})

	// Define step mappings
	ctx.Step(`^a multi-span trace with (\d+) spans$`, tc.givenMultiSpanTraceWithSpans)
	ctx.Step(`^a multi-span trace with one failed span$`, tc.givenMultiSpanTraceWithFailedSpan)
	ctx.Step(`^a multi-span trace with one slow span$`, tc.givenMultiSpanTraceWithSlowSpan)
	ctx.Step(`^a multi-span trace with mixed success and failure spans$`, tc.givenMultiSpanTraceWithMixedSpans)
	ctx.Step(`^an error occurs during trace processing$`, tc.givenErrorDuringTraceProcessing)
	ctx.Step(`^the trace is sent to the sampler$`, tc.whenTraceIsSentToSampler)
	ctx.Step(`^error is encountered$`, tc.whenErrorIsEncountered)
	ctx.Step(`^all spans should be processed and exported successfully$`, tc.thenAllSpansShouldBeProcessedSuccessfully)
	ctx.Step(`^the failed span should be properly handled$`, tc.thenFailedSpanShouldBeProperlyHandled)
	ctx.Step(`^the slow span should trigger latency-based sampling$`, tc.thenSlowSpanShouldTriggerLatencyBasedSampling)
	ctx.Step(`^all spans should be processed appropriately$`, tc.thenAllSpansShouldBeProcessedAppropriately)
	ctx.Step(`^error should be logged properly$`, tc.thenErrorShouldBeLoggedProperly)
	ctx.Step(`^the system should continue processing other traces$`, tc.thenSystemShouldContinueProcessingOtherTraces)
	ctx.Step(`^trace should be exported$`, tc.thenTraceShouldBeExported)
	ctx.Step(`^error metrics should be generated$`, tc.thenErrorMetricsShouldBeGenerated)
	ctx.Step(`^metrics should be generated$`, tc.thenMetricsShouldBeGenerated)
	ctx.Step(`^latency metrics should be generated$`, tc.thenLatencyMetricsShouldBeGenerated)
	ctx.Step(`^trace should still be exported$`, tc.thenTraceShouldStillBeExported)
}
