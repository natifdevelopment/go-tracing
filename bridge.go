package tracing

import (
	"context"

	"go.opentelemetry.io/otel/trace"

	"github.com/natifdevelopment/go-observability/logging/core"
)

// ContextWithSpanCarrier returns a context that contains both the OTel
// span context and a go-observability Carrier populated with the span's
// trace_id and span_id. This allows log records emitted via the
// go-observability logger to automatically include trace correlation IDs.
//
// Use this when you manually create spans (via tracing.StartSpan) and
// want the logger to pick up the trace_id:
//
//	ctx, span := tracing.StartSpan(ctx, "process-order")
//	defer span.End()
//	ctx = tracing.ContextWithSpanCarrier(ctx, span)
//	logger.Info(ctx, "processing order")  // trace_id auto-attached
func ContextWithSpanCarrier(ctx context.Context, span trace.Span) context.Context {
	if span == nil {
		return ctx
	}
	sc := span.SpanContext()
	if !sc.IsValid() {
		return ctx
	}

	carrier := core.Carrier{
		TraceID: sc.TraceID().String(),
	}
	// Merge with any existing carrier fields (e.g. user_id set earlier).
	existing := core.CarrierFrom(ctx)
	return core.WithCarrier(ctx, core.MergeCarrier(existing, carrier))
}

// SpanContextFromCarrier extracts a trace ID from a go-observability Carrier
// in the context. This is useful for bridging the Carrier's trace_id back
// to OTel when a span was not created by OTel (e.g. trace_id was set
// manually or from a legacy X-Request-Id header).
func SpanContextFromCarrier(ctx context.Context) string {
	return core.TraceID(ctx)
}
