package observability

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/tobilg/neoserver/internal/cache"
)

func TestRegisterCacheMetricsPublishesEveryCacheType(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() { otel.SetMeterProvider(previous) })

	if err := RegisterCacheMetrics(nil); err != nil {
		t.Fatalf("nil manager: %v", err)
	}
	manager, err := cache.NewManager(cache.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := RegisterCacheMetrics(manager); err != nil {
		t.Fatal(err)
	}
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatal(err)
	}
	types := map[string]bool{}
	names := map[string]bool{}
	for _, scope := range collected.ScopeMetrics {
		for _, instrument := range scope.Metrics {
			names[instrument.Name] = true
			if gauge, ok := instrument.Data.(metricdata.Gauge[int64]); ok && instrument.Name == "neoserver.cache.max_bytes" {
				for _, point := range gauge.DataPoints {
					value, _ := point.Attributes.Value(attribute.Key("cache.type"))
					types[value.AsString()] = true
				}
			}
		}
	}
	for _, name := range []string{"neoserver.cache.used_bytes", "neoserver.cache.max_bytes", "neoserver.cache.keys_evicted", "neoserver.cache.invalidation_keys"} {
		if !names[name] {
			t.Errorf("instrument %s was not published", name)
		}
	}
	for _, cacheType := range []string{"capabilities", "collections", "features", "tiles", "counts"} {
		if !types[cacheType] {
			t.Errorf("cache type %s has no data point", cacheType)
		}
	}
}
