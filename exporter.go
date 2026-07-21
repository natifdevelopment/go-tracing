package tracing

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/zipkin"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// createExporter builds a SpanExporter based on the configured ExporterType.
func createExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	switch cfg.ExporterType {
	case ExporterTypeOTLP, ExporterTypeJaeger, ExporterTypeTempo:
		return otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
			otlptracegrpc.WithInsecure(),
		)

	case ExporterTypeOTLPHTTP:
		return otlptracehttp.New(ctx,
			otlptracehttp.WithEndpoint(cfg.OTLPEndpoint),
			otlptracehttp.WithInsecure(),
		)

	case ExporterTypeZipkin:
		if cfg.ZipkinEndpoint == "" {
			return nil, fmt.Errorf("tracing: Zipkin endpoint not configured")
		}
		return zipkin.New(cfg.ZipkinEndpoint)

	case ExporterTypeCustom:
		if cfg.CustomExporter == nil {
			return nil, fmt.Errorf("tracing: custom exporter selected but CustomExporter is nil")
		}
		return cfg.CustomExporter, nil

	case ExporterTypeNoop, "":
		return nil, nil

	default:
		return nil, fmt.Errorf("tracing: unknown exporter type %q", cfg.ExporterType)
	}
}

// exporterTimeout returns the context timeout for exporter creation.
func exporterTimeout() time.Duration {
	return 10 * time.Second
}
