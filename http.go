package tracing

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/natifdevelopment/go-observability/logging/core"
)

// Transport wraps an http.RoundTripper with OpenTelemetry trace context
// propagation. When an HTTP client uses this transport, the current span's
// trace context is injected into outbound request headers (W3C traceparent),
// allowing downstream services to continue the trace.
//
// Additionally, if the request context contains a go-observability Carrier
// with a trace_id but no OTel span, the Carrier's trace_id is injected via
// the X-Request-Id header for backward compatibility with services that
// don't yet use OTel.
//
// Usage:
//
//	client := &http.Client{
//	    Transport: &tracing.Transport{Base: http.DefaultTransport},
//	}
type Transport struct {
	Base http.RoundTripper
}

// RoundTrip implements http.RoundTripper, injecting trace context into
// outbound request headers and creating a client span.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}

	ctx := req.Context()

	// Create a client span for this outbound request.
	tracer := otel.Tracer("go-tracing")
	ctx, span := tracer.Start(ctx, req.Method+" "+req.URL.Host,
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	// Inject the OTel trace context into request headers (W3C traceparent).
	propagator := otel.GetTextMapPropagator()
	propagator.Inject(ctx, propagation.HeaderCarrier(req.Header))

	// Also inject X-Request-Id from the logger Carrier for backward compat.
	carrier := core.CarrierFrom(ctx)
	if carrier.TraceID != "" {
		if req.Header.Get("X-Request-Id") == "" {
			req.Header.Set("X-Request-Id", carrier.TraceID)
		}
	}

	// Update the request with the span context.
	req = req.WithContext(ctx)

	resp, err := base.RoundTrip(req)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	// Record response status on the span.
	span.SetAttributes(
		attribute.Int("http.status_code", resp.StatusCode),
	)
	if resp.StatusCode >= 400 {
		span.SetStatus(codes.Error, resp.Status)
	}

	return resp, nil
}

// NewClient returns an *http.Client whose transport is wrapped with
// OpenTelemetry trace propagation.
func NewClient(base http.RoundTripper) *http.Client {
	if base == nil {
		base = http.DefaultTransport
	}
	return &http.Client{
		Transport: &Transport{Base: base},
	}
}

// WrapTransport wraps an existing http.RoundTripper with trace propagation.
func WrapTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &Transport{Base: base}
}

// InjectTraceHeaders injects W3C traceparent and X-Request-Id headers
// into an outbound request based on the context's OTel span and/or
// logger Carrier. This is useful for manual header injection when
// using a transport wrapper is not practical (e.g. httputil.ReverseProxy).
func InjectTraceHeaders(ctx context.Context, headers http.Header) http.Header {
	if headers == nil {
		headers = http.Header{}
	}

	// Inject W3C traceparent from OTel span context.
	propagator := otel.GetTextMapPropagator()
	propagator.Inject(ctx, propagation.HeaderCarrier(headers))

	// Inject X-Request-Id from logger Carrier for backward compat.
	carrier := core.CarrierFrom(ctx)
	if carrier.TraceID != "" {
		if headers.Get("X-Request-Id") == "" {
			headers.Set("X-Request-Id", carrier.TraceID)
		}
	}

	return headers
}
