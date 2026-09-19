package observability

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/tobilg/neoserver/internal/cache"
)

type Shutdown func(context.Context) error

// InitOTLP configures OTLP/HTTP metrics and traces using standard OTel environment variables.
func InitOTLP(ctx context.Context, serviceName string) (Shutdown, error) {
	res, err := resource.New(ctx, resource.WithAttributes(attribute.String("service.name", serviceName)))
	if err != nil {
		return nil, err
	}
	traceExporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	metricExporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExporter), sdktrace.WithResource(res))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)), sdkmetric.WithResource(res))
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	return func(ctx context.Context) error { return errors.Join(mp.Shutdown(ctx), tp.Shutdown(ctx)) }, nil
}

// RegisterCacheMetrics publishes cache capacity and pressure as asynchronous
// instruments so collection does not add work to request paths.
func RegisterCacheMetrics(manager *cache.Manager) error {
	if manager == nil {
		return nil
	}
	meter := otel.Meter("github.com/tobilg/neoserver/cache")
	used, err := meter.Int64ObservableGauge("neoserver.cache.used_bytes")
	if err != nil {
		return err
	}
	maximum, err := meter.Int64ObservableGauge("neoserver.cache.max_bytes")
	if err != nil {
		return err
	}
	dropped, err := meter.Int64ObservableCounter("neoserver.cache.sets_dropped")
	if err != nil {
		return err
	}
	rejected, err := meter.Int64ObservableCounter("neoserver.cache.sets_rejected")
	if err != nil {
		return err
	}
	evicted, err := meter.Int64ObservableCounter("neoserver.cache.keys_evicted")
	if err != nil {
		return err
	}
	bytesEvicted, err := meter.Int64ObservableCounter("neoserver.cache.bytes_evicted")
	if err != nil {
		return err
	}
	getsDropped, err := meter.Int64ObservableCounter("neoserver.cache.gets_dropped")
	if err != nil {
		return err
	}
	oversized, err := meter.Int64ObservableCounter("neoserver.cache.oversized_skipped")
	if err != nil {
		return err
	}
	shared, err := meter.Int64ObservableCounter("neoserver.cache.loads_shared")
	if err != nil {
		return err
	}
	loadErrors, err := meter.Int64ObservableCounter("neoserver.cache.load_errors")
	if err != nil {
		return err
	}
	ttlExpired, err := meter.Int64ObservableCounter("neoserver.cache.ttl_expired")
	if err != nil {
		return err
	}
	invalidationKeys, err := meter.Int64ObservableGauge("neoserver.cache.invalidation_keys")
	if err != nil {
		return err
	}
	_, err = meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		all := manager.GetMetrics()
		values := []struct {
			name    string
			metrics cache.Metrics
		}{
			{"capabilities", all.Capabilities}, {"collections", all.Collections}, {"features", all.Features}, {"tiles", all.Tiles}, {"counts", all.Counts},
		}
		for _, value := range values {
			options := metric.WithAttributes(attribute.String("cache.type", value.name))
			observer.ObserveInt64(used, value.metrics.SizeBytes, options)
			observer.ObserveInt64(maximum, value.metrics.MaxSizeBytes, options)
			observer.ObserveInt64(dropped, int64(value.metrics.SetsDropped), options)
			observer.ObserveInt64(rejected, int64(value.metrics.SetsRejected), options)
			observer.ObserveInt64(evicted, int64(value.metrics.KeysEvicted), options)
			observer.ObserveInt64(bytesEvicted, int64(value.metrics.BytesEvicted), options)
			observer.ObserveInt64(getsDropped, int64(value.metrics.GetsDropped), options)
			observer.ObserveInt64(oversized, value.metrics.OversizedSkipped, options)
			observer.ObserveInt64(shared, value.metrics.LoadsShared, options)
			observer.ObserveInt64(loadErrors, value.metrics.LoadErrors, options)
			observer.ObserveInt64(ttlExpired, value.metrics.TTLExpired, options)
			observer.ObserveInt64(invalidationKeys, value.metrics.InvalidationKeys, options)
		}
		return nil
	}, used, maximum, dropped, rejected, evicted, bytesEvicted, getsDropped, oversized, shared, loadErrors, ttlExpired, invalidationKeys)
	return err
}
