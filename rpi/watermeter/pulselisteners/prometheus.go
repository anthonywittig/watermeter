package pulselisteners

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type PrometheusRecorder struct {
	gallonCounter  prometheus.Counter
	lastPulseGauge prometheus.Gauge
}

func NewPrometheusRecorder() *PrometheusRecorder {
	gallonCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "gallons",
	})
	prometheus.MustRegister(gallonCounter)

	// Lets dashboards/health checks spot a silent meter (sensor fault, wiring,
	// or a wedged pipeline) without shell access to the journal or DB.
	lastPulseGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "last_pulse_timestamp_seconds",
		Help: "Unix time of the most recent meter pulse.",
	})
	prometheus.MustRegister(lastPulseGauge)

	return &PrometheusRecorder{
		gallonCounter:  gallonCounter,
		lastPulseGauge: lastPulseGauge,
	}
}

func (p *PrometheusRecorder) HandlePulse(recordedAt time.Time) error {
	p.gallonCounter.Add(0.1)
	p.lastPulseGauge.Set(float64(recordedAt.Unix()))
	return nil
}
