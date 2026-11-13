package metrics

import (
	"context"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	telemetrymetrics "github.com/inngest/inngest/pkg/telemetry/metrics"
)

func TestSetupOTelMeterExposesQueueLatency(t *testing.T) {
	ctx := context.Background()

	shutdown, err := SetupOTelMeter(ctx)
	require.NoError(t, err)
	t.Cleanup(shutdown)

	telemetrymetrics.HistogramQueueItemLatency(ctx, 42, telemetrymetrics.HistogramOpt{
		PkgName: "queue",
		Tags:    map[string]any{"kind": "test", "queue_shard": "test"},
	})

	families, err := registry.Gather()
	require.NoError(t, err)

	var found *dto.MetricFamily
	for _, f := range families {
		if f.GetName() == "inngest_queue_item_latency_duration_milliseconds" {
			found = f
			break
		}
	}
	require.NotNil(t, found, "queue latency histogram missing from /metrics registry")
	require.Equal(t, dto.MetricType_HISTOGRAM, found.GetType())
	require.GreaterOrEqual(t, found.GetMetric()[0].GetHistogram().GetSampleCount(), uint64(1))
}
