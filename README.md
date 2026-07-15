# go-tracing

OpenTelemetry tracing initialization and middleware for Go microservices.

Bridges OpenTelemetry distributed tracing with the
[go-observability](https://github.com/natifdevelopment/go-observability) logger
Carrier system, so that `trace_id` and `span_id` from OTel spans automatically
appear in structured log records.

## Features

- **Multiple exporter backends** — OTLP gRPC, OTLP HTTP, Jaeger, Grafana Tempo, Zipkin, Noop, Custom
- **Gin middleware** — creates a span per HTTP request, propagates W3C traceparent
- **HTTP transport wrapper** — injects trace context into outbound requests
- **Logger Carrier bridge** — OTel trace_id flows into go-observability log records
- **Environment-based config** — all settings via env vars
- **Graceful degradation** — `OTEL_TRACES_ENABLED=false` or `OTEL_TRACES_EXPORTER=noop` = zero-overhead no-op
- **Custom exporter support** — plug in your own `sdktrace.SpanExporter`

## Quick Start

```go
import "github.com/natifdevelopment/go-tracing"

// Initialize tracer (call once at startup)
tp, err := tracing.Init(tracing.Config{
    ServiceName:    "my-service",
    ServiceVersion: "1.0.0",
    Environment:    "production",
    ExporterType:   tracing.ExporterTypeOTLP,
    OTLPEndpoint:   "localhost:4317",
})
if err != nil {
    log.Fatalf("tracing init failed: %v", err)
}
defer tp.Shutdown(context.Background())

// Gin middleware
r.Use(tracing.GinMiddleware("my-service"))
```

### Using a different exporter

```go
// Zipkin
tp, err := tracing.Init(tracing.Config{
    ServiceName:    "my-service",
    ExporterType:   tracing.ExporterTypeZipkin,
    ZipkinEndpoint: "http://zipkin:9411/api/v2/spans",
})

// Jaeger (via OTLP gRPC — Jaeger natively accepts OTLP on :4317)
tp, err := tracing.Init(tracing.Config{
    ServiceName:    "my-service",
    ExporterType:   tracing.ExporterTypeJaeger,
    OTLPEndpoint:   "jaeger:4317",
})

// Grafana Tempo (via OTLP gRPC)
tp, err := tracing.Init(tracing.Config{
    ServiceName:    "my-service",
    ExporterType:   tracing.ExporterTypeTempo,
    OTLPEndpoint:   "tempo:4317",
})

// OTLP HTTP
tp, err := tracing.Init(tracing.Config{
    ServiceName:    "my-service",
    ExporterType:   tracing.ExporterTypeOTLPHTTP,
    OTLPEndpoint:   "localhost:4318",
})

// Noop (zero overhead)
tp, err := tracing.Init(tracing.Config{
    ServiceName:    "my-service",
    ExporterType:   tracing.ExporterTypeNoop,
})

// Custom exporter
tp, err := tracing.Init(tracing.Config{
    ServiceName:    "my-service",
    ExporterType:   tracing.ExporterTypeCustom,
    CustomExporter: mySpanExporter,
})
```

## Exporter Types

| Type | Backend | Protocol | Default Endpoint |
|---|---|---|---|
| `otlp` (default) | OTLP gRPC | gRPC | `localhost:4317` |
| `otlphttp` | OTLP HTTP | HTTP | `localhost:4318` |
| `jaeger` | Jaeger via OTLP gRPC | gRPC | `localhost:4317` |
| `tempo` | Grafana Tempo via OTLP gRPC | gRPC | `localhost:4317` |
| `zipkin` | Zipkin native | HTTP | `http://localhost:9411/api/v2/spans` |
| `noop` | No-op tracer | — | — |
| `custom` | User-provided `SpanExporter` | — | — |

> **Note:** The native Jaeger exporter was deprecated by OpenTelemetry in July 2023.
> Jaeger and Tempo both natively accept OTLP, so they use the OTLP gRPC exporter
> under the hood with the appropriate endpoint.

## Configuration (Environment Variables)

| Variable | Default | Description |
|---|---|---|
| `OTEL_TRACES_EXPORTER` | `otlp` | Exporter type: `otlp`\|`otlphttp`\|`jaeger`\|`tempo`\|`zipkin`\|`noop`\|`custom` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | OTLP gRPC/HTTP endpoint |
| `OTEL_EXPORTER_ZIPKIN_ENDPOINT` | `http://localhost:9411/api/v2/spans` | Zipkin collector URL |
| `OTEL_SERVICE_NAME` | `unknown` | Service name |
| `SERVICE_NAME` | `unknown` | Fallback for service name |
| `SERVICE_VERSION` | `0.0.0` | Service version |
| `ENVIRONMENT` | `development` | Deployment environment |
| `OTEL_TRACES_SAMPLE_RATE` | `1.0` | Sampling ratio (0.0–1.0) |
| `OTEL_TRACES_ENABLED` | `true` | Enable/disable tracing |

## API

### `tracing.Init(cfg) (*TracerProvider, error)`
Initializes the global OTel TracerProvider. Exporter is selected via `cfg.ExporterType`.

### `tracing.InitFromEnv() (*TracerProvider, error)`
Convenience: loads config from env vars and calls `Init`.

### `tracing.GinMiddleware(serviceName) gin.HandlerFunc`
Gin middleware that creates a server span per request, extracts W3C traceparent,
and injects trace_id into the go-observability Carrier.

### `tracing.Transport`
`http.RoundTripper` wrapper that creates client spans and injects trace context
into outbound headers.

### `tracing.NewClient(base) *http.Client`
Returns an `*http.Client` with trace propagation.

### `tracing.InjectTraceHeaders(ctx, headers) http.Header`
Manually inject W3C traceparent + X-Request-Id into outbound headers.

### `tracing.StartSpan(ctx, name) (context.Context, trace.Span)`
Start a span from the global tracer.

### `tracing.ContextWithSpanCarrier(ctx, span) context.Context`
Bridge: injects OTel span's trace_id into the go-observability Carrier.

### `tracing.TraceIDFromContext(ctx) string`
Extract trace ID from OTel span context.

## License

MIT
