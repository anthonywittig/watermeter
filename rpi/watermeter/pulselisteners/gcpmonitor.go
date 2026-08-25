package pulselisteners

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	monitoring "cloud.google.com/go/monitoring/apiv3"
	"github.com/golang/protobuf/ptypes/timestamp"
	"google.golang.org/api/option"
	"google.golang.org/genproto/googleapis/api/metric"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	"google.golang.org/genproto/googleapis/api/monitoredres"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

// gcpWriteTimeout bounds each CreateTimeSeries call; all handlers share one
// goroutine, so a hung RPC would otherwise stall every pulse listener
// indefinitely.
const gcpWriteTimeout = 30 * time.Second

type GcpMonitor struct {
	ctx                   context.Context
	gcpClient             *monitoring.MetricClient
	db                    *sql.DB
	earliestNotRecordedAt time.Time
	projectID             string
	pulsesNotRecorded     int
}

func NewGcpMonitor(ctx context.Context, db *sql.DB, gcpProjectID string, credentialsFile string) (*GcpMonitor, error) {
	gcpClient, err := monitoring.NewMetricClient(ctx, option.WithCredentialsFile(credentialsFile))
	if err != nil {
		return nil, err
	}

	return &GcpMonitor{
		ctx:       ctx,
		db:        db,
		gcpClient: gcpClient,
		projectID: gcpProjectID,
	}, nil
}

func (g *GcpMonitor) HandlePulse(recordedAt time.Time) error {
	if g.pulsesNotRecorded == 0 {
		g.earliestNotRecordedAt = recordedAt
	}
	g.pulsesNotRecorded++

	if time.Now().Sub(g.earliestNotRecordedAt).Seconds() < 30 {
		// We're ok delaying a bit. GCP has a 10 second max reporting rate.
		return nil
	}

	req := buildCreateTimeSeriesRequest(g.projectID, g.earliestNotRecordedAt, recordedAt, g.pulsesNotRecorded)

	ctx, cancel := context.WithTimeout(g.ctx, gcpWriteTimeout)
	defer cancel()
	if err := g.gcpClient.CreateTimeSeries(ctx, req); err != nil {
		return fmt.Errorf("could not write time series value, %v ", err)
	}

	g.earliestNotRecordedAt = time.Time{}
	g.pulsesNotRecorded = 0

	return nil
}

func buildCreateTimeSeriesRequest(projectID string, earliest time.Time, latest time.Time, pulses int) *monitoringpb.CreateTimeSeriesRequest {
	// Cumulative metrics require start < end; two pulses landing in the same
	// epoch second after a quiet spell would otherwise be rejected
	// ("The start time must be before the end time").
	startSec := earliest.Unix()
	endSec := latest.Unix()
	if endSec <= startSec {
		endSec = startSec + 1
	}

	return &monitoringpb.CreateTimeSeriesRequest{
		Name: "projects/" + projectID,
		TimeSeries: []*monitoringpb.TimeSeries{
			{
				Metric: &metricpb.Metric{
					Type: "custom.googleapis.com/watermeter/gallons",
				},
				MetricKind: metric.MetricDescriptor_CUMULATIVE,
				Resource: &monitoredres.MonitoredResource{
					Type: "global",
					Labels: map[string]string{
						"project_id": projectID,
					},
				},
				Points: []*monitoringpb.Point{
					{
						Interval: &monitoringpb.TimeInterval{
							StartTime: &timestamp.Timestamp{Seconds: startSec},
							EndTime:   &timestamp.Timestamp{Seconds: endSec},
						},
						Value: &monitoringpb.TypedValue{
							Value: &monitoringpb.TypedValue_DoubleValue{
								DoubleValue: 0.1 * float64(pulses),
							},
						},
					},
				},
			},
		},
	}
}
