package tracing

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/natifdevelopment/go-observability/logging/core"
)

// GinMiddleware returns a Gin middleware that creates an OpenTelemetry span
// for each HTTP request. The span is named after the HTTP method and route
// template (e.g. "GET /api/v1/users/:id").
//
// The middleware also:
//   - Extracts trace context from incoming W3C traceparent headers
//   - Injects the OTel trace_id into the go-observability Carrier on the
//     request context, so all log records emitted during the request
//     automatically carry trace_id and span_id
//   - Records HTTP method, route, status code, and duration as span attributes
//   - Sets span status to ERROR for 5xx responses
func GinMiddleware(serviceName string) gin.HandlerFunc {
	tracer := otel.Tracer(serviceName)
	propagator := otel.GetTextMapPropagator()

	return func(c *gin.Context) {
		// Extract trace context from incoming headers (W3C traceparent).
		ctx := propagator.Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))

		// Start a server span.
		spanName := fmt.Sprintf("%s %s", c.Request.Method, c.Request.URL.Path)
		ctx, span := tracer.Start(ctx, spanName,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.method", c.Request.Method),
				attribute.String("http.target", c.Request.URL.Path),
				attribute.String("http.scheme", c.Request.URL.Scheme),
				attribute.String("net.host.name", c.Request.Host),
			),
		)

		// Inject OTel trace_id into the logger Carrier so all log records
		// during this request carry trace_id and span_id.
		sc := span.SpanContext()
		if sc.HasTraceID() {
			carrier := core.Carrier{
				TraceID: sc.TraceID().String(),
			}
			// Preserve any existing carrier fields (e.g. from upstream proxy).
			existing := core.CarrierFrom(ctx)
			ctx = core.WithCarrier(ctx, core.MergeCarrier(existing, carrier))
		}

		// Replace the request context with the span context.
		c.Request = c.Request.WithContext(ctx)

		// Store trace_id in gin context for backward compatibility with
		// existing middleware that reads c.Get("trace_id").
		if sc.HasTraceID() {
			c.Set("trace_id", sc.TraceID().String())
			c.Set("span_id", sc.SpanID().String())
		}

		// Propagate trace context to the response header so downstream
		// callers or clients can correlate.
		if sc.HasTraceID() {
			c.Writer.Header().Set("X-Trace-Id", sc.TraceID().String())
		}

		defer span.End()

		start := time.Now()
		c.Next()
		elapsed := time.Since(start)

		// Use the matched route template if available (after c.Next() so
		// Gin has resolved the route).
		route := c.FullPath()
		if route == "" {
			route = "unknown"
		}

		// Record response attributes.
		status := c.Writer.Status()
		span.SetAttributes(
			attribute.Int("http.status_code", status),
			attribute.Int("http.response_content_length", c.Writer.Size()),
			attribute.String("http.route", route),
			attribute.Float64("http.duration_ms", float64(elapsed.Microseconds())/1000.0),
		)

		// Update span name to use the route template for better cardinality.
		span.SetName(fmt.Sprintf("%s %s", c.Request.Method, route))

		// Set span status for error responses.
		if status >= 500 {
			span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", status))
			span.RecordError(fmt.Errorf("server error: status %d", status))
		} else if status >= 400 {
			span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", status))
		}
	}
}
