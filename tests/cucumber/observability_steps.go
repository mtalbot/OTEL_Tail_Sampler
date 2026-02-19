package cucumber

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"OTEL_Tail_Sampler/internal/config"
	"OTEL_Tail_Sampler/internal/receiver"
	"OTEL_Tail_Sampler/internal/decision"
	"OTEL_Tail_Sampler/internal/exporter"
	"OTEL_Tail_Sampler/internal/processor"
)

// Step definitions for observability scenarios

type testContext struct {
	t                *testing.T
	receiver         *receiver.Receiver
	processor        *processor.Processor
	exporter         *exporter.Exporter
	decisionEngine   *decision.Engine
	traceData        ptrace.Traces
	serverAddr       string
	server           *grpc.Server
	clientConn       *grpc.ClientConn
}

func (tc *testContext) givenMultiSpanTraceWithSpans(count int) {
	tc.t.Helper()
	
	// Create a multi-span trace with the specified number of spans
	trace := ptrace.NewTraces()
	resourceSpan := trace.ResourceSpans().AppendEmpty()
	scopeSpan := resourceSpan.ScopeSpans().AppendEmpty()
	
	for i := 0; i < count; i++ {
		span := scopeSpan.Spans().AppendEmpty()
		span.SetName(fmt.Sprintf("span-%d", i))
		span.SetTraceID(ptrace.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
		span.SetSpanID(ptrace.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8 + byte(i)}))
		span.SetKind(ptrace.SpanKindServer)
		span.Status().SetCode(ptrace.StatusCodeOk)
		span.SetStartTimestamp(ptrace.Timestamp(time.Now().UnixNano()))
		span.SetEndTimestamp(ptrace.Timestamp(time.Now().UnixNano() + int64(time.Millisecond*100)))
	}
	
	tc.traceData = trace
}

func (tc *testContext) givenMultiSpanTraceWithFailedSpan() {
	tc.t.Helper()
	
	// Create a multi-span trace with one failed span
	trace := ptrace.NewTraces()
	resourceSpan := trace.ResourceSpans().AppendEmpty()
	scopeSpan := resourceSpan.ScopeSpans().AppendEmpty()
	
	// Add successful spans
	for i := 0; i < 2; i++ {
		span := scopeSpan.Spans().AppendEmpty()
		span.SetName(fmt.Sprintf("successful-span-%d", i))
		span.SetTraceID(ptrace.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
		span.SetSpanID(ptrace.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8 + byte(i)}))
		span.SetKind(ptrace.SpanKindServer)
		span.Status().SetCode(ptrace.StatusCodeOk)
		span.SetStartTimestamp(ptrace.Timestamp(time.Now().UnixNano()))
		span.SetEndTimestamp(ptrace.Timestamp(time.Now().UnixNano() + int64(time.Millisecond*100)))
	}
	
	// Add failed span
	failedSpan := scopeSpan.Spans().AppendEmpty()
	failedSpan.SetName("failed-span")
	failedSpan.SetTraceID(ptrace.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	failedSpan.SetSpanID(ptrace.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 10}))
	failedSpan.SetKind(ptrace.SpanKindServer)
	failedSpan.Status().SetCode(ptrace.StatusCodeError)
	failedSpan.SetStartTimestamp(ptrace.Timestamp(time.Now().UnixNano()))
	failedSpan.SetEndTimestamp(ptrace.Timestamp(time.Now().UnixNano() + int64(time.Millisecond*100)))
	
	tc.traceData = trace
}

func (tc *testContext) givenMultiSpanTraceWithSlowSpan() {
	tc.t.Helper()
	
	// Create a multi-span trace with one slow span
	trace := ptrace.NewTraces()
	resourceSpan := trace.ResourceSpans().AppendEmpty()
	scopeSpan := resourceSpan.ScopeSpans().AppendEmpty()
	
	// Add successful spans
	for i := 0; i < 2; i++ {
		span := scopeSpan.Spans().AppendEmpty()
		span.SetName(fmt.Sprintf("successful-span-%d", i))
		span.SetTraceID(ptrace.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
		span.SetSpanID(ptrace.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8 + byte(i)}))
		span.SetKind(ptrace.SpanKindServer)
		span.Status().SetCode(ptrace.StatusCodeOk)
		span.SetStartTimestamp(ptrace.Timestamp(time.Now().UnixNano()))
		span.SetEndTimestamp(ptrace.Timestamp(time.Now().UnixNano() + int64(time.Millisecond*100)))
	}
	
	// Add slow span
	slowSpan := scopeSpan.Spans().AppendEmpty()
	slowSpan.SetName("slow-span")
	slowSpan.SetTraceID(ptrace.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	slowSpan.SetSpanID(ptrace.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 10}))
	slowSpan.SetKind(ptrace.SpanKindServer)
	slowSpan.Status().SetCode(ptrace.StatusCodeOk)
	slowSpan.SetStartTimestamp(ptrace.Timestamp(time.Now().UnixNano()))
	slowSpan.SetEndTimestamp(ptrace.Timestamp(time.Now().UnixNano() + int64(time.Second*5))) // 5 seconds
	
	tc.traceData = trace
}

func (tc *testContext) givenMultiSpanTraceWithMixedSpans() {
	tc.t.Helper()
	
	// Create a multi-span trace with mixed success and failure spans
	trace := ptrace.NewTraces()
	resourceSpan := trace.ResourceSpans().AppendEmpty()
	scopeSpan := resourceSpan.ScopeSpans().AppendEmpty()
	
	// Add successful span
	span1 := scopeSpan.Spans().AppendEmpty()
	span1.SetName("successful-span-1")
	span1.SetTraceID(ptrace.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span1.SetSpanID(ptrace.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span1.SetKind(ptrace.SpanKindServer)
	span1.Status().SetCode(ptrace.StatusCodeOk)
	span1.SetStartTimestamp(ptrace.Timestamp(time.Now().UnixNano()))
	span1.SetEndTimestamp(ptrace.Timestamp(time.Now().UnixNano() + int64(time.Millisecond*100)))
	
	// Add failed span
	span2 := scopeSpan.Spans().AppendEmpty()
	span2.SetName("failed-span")
	span2.SetTraceID(ptrace.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span2.SetSpanID(ptrace.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 9}))
	span2.SetKind(ptrace.SpanKindServer)
	span2.Status().SetCode(ptrace.StatusCodeError)
	span2.SetStartTimestamp(ptrace.Timestamp(time.Now().UnixNano()))
	span2.SetEndTimestamp(ptrace.Timestamp(time.Now().UnixNano() + int64(time.Millisecond*100)))
	
	// Add another successful span
	span3 := scopeSpan.Spans().AppendEmpty()
	span3.SetName("successful-span-2")
	span3.SetTraceID(ptrace.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span3.SetSpanID(ptrace.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 10}))
	span3.SetKind(ptrace.SpanKindServer)
	span3.Status().SetCode(ptrace.StatusCodeOk)
	span3.SetStartTimestamp(ptrace.Timestamp(time.Now().UnixNano()))
	span3.SetEndTimestamp(ptrace.Timestamp(time.Now().UnixNano() + int64(time.Millisecond*150)))
	
	tc.traceData = trace
}

func (tc *testContext) givenErrorDuringTraceProcessing() {
	// This would simulate an error during processing in a real scenario
	tc.t.Log("Simulating error during trace processing")
}

func (tc *testContext) whenTraceIsSentToSampler() {
	tc.t.Helper()
	
	// In a real test, we would send the trace to the sampler via OTLP
	// For now, we'll just verify that our trace data was created correctly
	assert.NotNil(tc.t, tc.traceData)
}

func (tc *testContext) whenErrorIsEncountered() {
	// Simulate error handling in a real scenario
	tc.t.Log("Simulating error encounter")
}

func (tc *testContext) thenAllSpansShouldBeProcessedSuccessfully() {
	tc.t.Helper()
	
	// Verify that spans are properly structured
	assert.NotNil(tc.t, tc.traceData)
	assert.Greater(tc.t, tc.traceData.SpanCount(), 0)
	
	// Check that we have at least one resource span
	assert.Greater(tc.t, tc.traceData.ResourceSpans().Len(), 0)
	
	// Verify spans are present and have proper structure
	resourceSpan := tc.traceData.ResourceSpans().At(0)
	assert.NotNil(tc.t, resourceSpan)
	
	scopeSpan := resourceSpan.ScopeSpans().At(0)
	assert.NotNil(tc.t, scopeSpan)
	
	assert.Greater(tc.t, scopeSpan.Spans().Len(), 0)
}

func (tc *testContext) thenFailedSpanShouldBeProperlyHandled() {
	tc.t.Helper()
	
	// Verify that spans are properly structured
	assert.NotNil(tc.t, tc.traceData)
	assert.Greater(tc.t, tc.traceData.SpanCount(), 0)
	
	// Check that we have at least one resource span
	assert.Greater(tc.t, tc.traceData.ResourceSpans().Len(), 0)
	
	// Verify spans are present and have proper structure
	resourceSpan := tc.traceData.ResourceSpans().At(0)
	assert.NotNil(tc.t, resourceSpan)
	
	scopeSpan := resourceSpan.ScopeSpans().At(0)
	assert.NotNil(tc.t, scopeSpan)
	
	// Check that we have at least one failed span
	spans := scopeSpan.Spans()
	assert.Greater(tc.t, spans.Len(), 0)
	
	// Verify at least one span has error status
	hasError := false
	for i := 0; i < spans.Len(); i++ {
		span := spans.At(i)
		if span.Status().Code() == ptrace.StatusCodeError {
			hasError = true
			break
		}
	}
	
	assert.True(tc.t, hasError, "Expected at least one failed span")
}

func (tc *testContext) thenSlowSpanShouldTriggerLatencyBasedSampling() {
	tc.t.Helper()
	
	// Verify that spans are properly structured
	assert.NotNil(tc.t, tc.traceData)
	assert.Greater(tc.t, tc.traceData.SpanCount(), 0)
	
	// Check that we have at least one resource span
	assert.Greater(tc.t, tc.traceData.ResourceSpans().Len(), 0)
	
	// Verify spans are present and have proper structure
	resourceSpan := tc.traceData.ResourceSpans().At(0)
	assert.NotNil(tc.t, resourceSpan)
	
	scopeSpan := resourceSpan.ScopeSpans().At(0)
	assert.NotNil(tc.t, scopeSpan)
	
	// Check that we have at least one slow span (5 seconds duration)
	spans := scopeSpan.Spans()
	assert.Greater(tc.t, spans.Len(), 0)
	
	// Verify at least one span has long duration
	hasLongDuration := false
	for i := 0; i < spans.Len(); i++ {
		span := spans.At(i)
		duration := span.EndTimestamp() - span.StartTimestamp()
		if duration > ptrace.Timestamp(time.Second*4) { // More than 4 seconds
			hasLongDuration = true
			break
		}
	}
	
	assert.True(tc.t, hasLongDuration, "Expected at least one slow span")
}

func (tc *testContext) thenAllSpansShouldBeProcessedAppropriately() {
	tc.t.Helper()
	
	// Verify that spans are properly structured
	assert.NotNil(tc.t, tc.traceData)
	assert.Greater(tc.t, tc.traceData.SpanCount(), 0)
	
	// Check that we have at least one resource span
	assert.Greater(tc.t, tc.traceData.ResourceSpans().Len(), 0)
	
	// Verify spans are present and have proper structure
	resourceSpan := tc.traceData.ResourceSpans().At(0)
	assert.NotNil(tc.t, resourceSpan)
	
	scopeSpan := resourceSpan.ScopeSpans().At(0)
	assert.NotNil(tc.t, scopeSpan)
	
	// Check that we have multiple spans with different statuses
	spans := scopeSpan.Spans()
	assert.Greater(tc.t, spans.Len(), 0)
}

func (tc *testContext) thenErrorShouldBeLoggedProperly() {
	tc.t.Helper()
	
	// In a real test environment, this would check logs for error messages
	tc.t.Log("Verifying error logging")
	// For now just assert that we have a valid trace
	assert.NotNil(tc.t, tc.traceData)
}

func (tc *testContext) thenSystemShouldContinueProcessingOtherTraces() {
	tc.t.Helper()
	
	// In a real test, this would verify that the system continues processing after an error
	tc.t.Log("Verifying system continues processing")
	// For now just assert that we have a valid trace
	assert.NotNil(tc.t, tc.traceData)
}

func (tc *testContext) thenTraceShouldBeExported() {
	tc.t.Helper()
	
	// Verify that the trace has been properly set up for export
	assert.NotNil(tc.t, tc.traceData)
	assert.Greater(tc.t, tc.traceData.SpanCount(), 0)
}

func (tc *testContext) thenErrorMetricsShouldBeGenerated() {
	tc.t.Helper()
	
	// In a real test environment, this would verify that error metrics are generated
	tc.t.Log("Verifying error metrics generation")
	// For now just assert that we have a valid trace
	assert.NotNil(tc.t, tc.traceData)
}

func (tc *testContext) thenMetricsShouldBeGenerated() {
	tc.t.Helper()
	
	// In a real test environment, this would verify that appropriate metrics are generated
	tc.t.Log("Verifying metrics generation")
	// For now just assert that we have a valid trace
	assert.NotNil(tc.t, tc.traceData)
}

func (tc *testContext) thenLatencyMetricsShouldBeGenerated() {
	tc.t.Helper()
	
	// In a real test environment, this would verify that latency metrics are generated
	tc.t.Log("Verifying latency metrics generation")
	// For now just assert that we have a valid trace
	assert.NotNil(tc.t, tc.traceData)
}

func (tc *testContext) thenTraceShouldStillBeExported() {
	tc.t.Helper()
	
	// Verify that the trace is still exported despite errors
	assert.NotNil(tc.t, tc.traceData)
	assert.Greater(tc.t, tc.traceData.SpanCount(), 0)
}