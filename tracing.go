// Package tracing provides OpenTelemetry tracing initialization and
// middleware for Go microservices.
//
// This package bridges OpenTelemetry distributed tracing with the
// go-observability logger Carrier system, so that trace_id
// and span_id from OTel spans automatically appear in structured log
// records.
//
// # Quick Start
//
//	// Initialize tracer (call once at startup, defer shutdown)
//	tp, err := tracing.Init(tracing.Config{
//	    ServiceName:    "my-service",
//	    ServiceVersion: "1.0.0",
//	    Environment:    "production",
//	    OTLPEndpoint:   "",
//	})
//	if err != nil {
//	    log.Fatalf("tracing init failed: %v", err)
//	}
//	defer tp.Shutdown(context.Background())
//
//	// Gin middleware (creates a span per request, propagates trace_id)
//	r.Use(tracing.GinMiddleware("my-service"))
//
// # Configuration via Environment Variables
//
//	OTEL_TRACES_EXPORTER         - exporter type: otlp|otlphttp|jaeger|tempo|zipkin|noop|custom (default: otlp)
//	OTEL_EXPORTER_OTLP_ENDPOINT  - OTLP gRPC/HTTP endpoint (default: "")
//	OTEL_EXPORTER_ZIPKIN_ENDPOINT - Zipkin collector URL (default: "")
//	OTEL_SERVICE_NAME            - Service name (default: unknown)
//	SERVICE_VERSION              - Service version (default: 0.0.0)
//	ENVIRONMENT                  - deployment environment (default: development)
//	OTEL_TRACES_SAMPLE_RATE      - sampling ratio 0.0-1.0 (default: 1.0)
//	OTEL_TRACES_ENABLED          - enable tracing (default: false)
//
// # Exporter Types
//
//   - otlp:     OTLP gRPC exporter (default, works with Jaeger, Tempo, OTEL Collector)
//   - otlphttp: OTLP HTTP exporter
//   - jaeger:   OTLP gRPC to Jaeger (Jaeger natively accepts OTLP on 4317)
//   - tempo:    OTLP gRPC to Grafana Tempo
//   - zipkin:   Zipkin native exporter
//   - noop:     no-op tracer (zero overhead)
//   - custom:   user-provided SpanExporter via Config.CustomExporter
package tracing

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// ExporterType selects which trace exporter to use.
type ExporterType string

const (
	// ExporterTypeOTLP exports spans via OTLP gRPC (default).
	ExporterTypeOTLP ExporterType = "otlp"
	// ExporterTypeOTLPHTTP exports spans via OTLP HTTP.
	ExporterTypeOTLPHTTP ExporterType = "otlphttp"
	// ExporterTypeJaeger exports spans via OTLP gRPC to a Jaeger collector.
	// Jaeger natively accepts OTLP on port 4317 (gRPC) / 4318 (HTTP).
	ExporterTypeJaeger ExporterType = "jaeger"
	// ExporterTypeTempo exports spans via OTLP gRPC to Grafana Tempo.
	ExporterTypeTempo ExporterType = "tempo"
	// ExporterTypeZipkin exports spans to a Zipkin collector.
	ExporterTypeZipkin ExporterType = "zipkin"
	// ExporterTypeNoop disables span export (no-op tracer).
	ExporterTypeNoop ExporterType = "noop"
	// ExporterTypeCustom uses a user-provided SpanExporter.
	ExporterTypeCustom ExporterType = "custom"
)

// TracingConfig is the global tracing configuration set by the application.
// When ServiceName is set, ConfigFromEnv returns it instead of reading
// environment variables, so services can centralize config in configs.* globals.
var TracingConfig Config

// Config holds tracing configuration.
type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	ExporterType   ExporterType
	OTLPEndpoint   string
	ZipkinEndpoint string
	SampleRate     float64
	Enabled        bool
	// CustomExporter is used when ExporterType is ExporterTypeCustom.
	CustomExporter sdktrace.SpanExporter
}

// ConfigFromEnv returns the tracing configuration.
// If the application has set TracingConfig.ServiceName, it is returned
// (with defaults applied for empty exporter/sample fields).
// Otherwise configuration is loaded from environment variables.
func ConfigFromEnv() Config {
	if TracingConfig.ServiceName != "" {
		cfg := TracingConfig
		if cfg.ExporterType == "" {
			cfg.ExporterType = ExporterTypeOTLP
		}
		if cfg.SampleRate == 0 {
			cfg.SampleRate = 1.0
		}
		return cfg
	}

	cfg := Config{
		ServiceName:    getEnv("OTEL_SERVICE_NAME", getEnv("SERVICE_NAME", "unknown")),
		ServiceVersion: getEnv("SERVICE_VERSION", "0.0.0"),
		Environment:    getEnv("ENVIRONMENT", "development"),
		ExporterType:   ExporterType(getEnv("OTEL_TRACES_EXPORTER", string(ExporterTypeOTLP))),
		OTLPEndpoint:   getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		ZipkinEndpoint: getEnv("OTEL_EXPORTER_ZIPKIN_ENDPOINT", ""),
		SampleRate:     getEnvFloat("OTEL_TRACES_SAMPLE_RATE", 1.0),
		Enabled:        getEnvBool("OTEL_TRACES_ENABLED", false),
	}
	return cfg
}

// TracerProvider wraps the OTel SDK TracerProvider with a clean Shutdown method.
type TracerProvider struct {
	tp *sdktrace.TracerProvider
}

// Tracer returns a named tracer from the underlying provider.
func (t *TracerProvider) Tracer(name string) trace.Tracer {
	if t == nil || t.tp == nil {
		return trace.NewNoopTracerProvider().Tracer(name)
	}
	return t.tp.Tracer(name)
}

// Shutdown gracefully flushes and shuts down the tracer provider.
func (t *TracerProvider) Shutdown(ctx context.Context) error {
	if t == nil || t.tp == nil {
		return nil
	}
	return t.tp.Shutdown(ctx)
}

// ForceFlush flushes all pending spans.
func (t *TracerProvider) ForceFlush(ctx context.Context) error {
	if t == nil || t.tp == nil {
		return nil
	}
	return t.tp.ForceFlush(ctx)
}

// Init initializes the global OpenTelemetry TracerProvider and returns
// a TracerProvider wrapper for graceful shutdown.
//
// If cfg.Enabled is false or cfg.ExporterType is ExporterTypeNoop, a no-op
// tracer provider is returned (zero overhead).
// The global OTel TextMapPropagator is set to W3C TraceContext + Baggage
// so that traceparent headers are automatically propagated across services.
func Init(cfg Config) (*TracerProvider, error) {
	if cfg.ExporterType == "" {
		cfg.ExporterType = ExporterTypeOTLP
	}

	if !cfg.Enabled || cfg.ExporterType == ExporterTypeNoop {
		noopTP := sdktrace.NewTracerProvider(
			sdktrace.WithSampler(sdktrace.NeverSample()),
		)
		otel.SetTracerProvider(noopTP)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))
		return &TracerProvider{tp: noopTP}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), exporterTimeout())
	defer cancel()

	exporter, err := createExporter(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("tracing: failed to create exporter: %w", err)
	}
	if exporter == nil {
		return nil, fmt.Errorf("tracing: exporter is nil for type %q", cfg.ExporterType)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			semconv.DeploymentEnvironment(cfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("tracing: failed to create resource: %w", err)
	}

	sampler := sdktrace.ParentBased(
		sdktrace.TraceIDRatioBased(cfg.SampleRate),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithMaxExportBatchSize(512),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &TracerProvider{tp: tp}, nil
}

// InitFromEnv is a convenience function that loads config from environment
// variables and calls Init.
func InitFromEnv() (*TracerProvider, error) {
	return Init(ConfigFromEnv())
}

// StartSpan starts a span from the global tracer and returns the span and
// a context containing it. The span name should describe the operation.
//
// Usage:
//
//	ctx, span := tracing.StartSpan(ctx, "process-order")
//	defer span.End()
func StartSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return otel.Tracer("go-tracing").Start(ctx, name)
}

// TraceIDFromContext returns the trace ID from the OTel span in ctx,
// or an empty string if no active span exists.
func TraceIDFromContext(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}

// SpanIDFromContext returns the span ID from the OTel span in ctx,
// or an empty string if no active span exists.
func SpanIDFromContext(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.SpanID().String()
}

// --- helpers ---

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		switch strings.ToLower(v) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		}
	}
	return fallback
}
