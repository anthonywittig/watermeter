package pulselisteners

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type PrometheusRecorder struct {
	gallonCounter prometheus.Counter
}

func NewPrometheusRecorder() *PrometheusRecorder {
	gallonCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "gallons",
	})
	prometheus.MustRegister(gallonCounter)
	return &PrometheusRecorder{
		gallonCounter: gallonCounter,
	}
}

func (p *PrometheusRecorder) HandlePulse(recordedAt time.Time) error {
	p.gallonCounter.Add(0.1)
	return nil
}
