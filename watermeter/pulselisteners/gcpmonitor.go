package pulselisteners

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	monitoring "cloud.google.com/go/monitoring/apiv3"
	"github.com/golang/protobuf/ptypes/timestamp"
	"google.golang.org/genproto/googleapis/api/metric"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	"google.golang.org/genproto/googleapis/api/monitoredres"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

type GcpMonitor struct {
	ctx                   context.Context
	gcpClient             *monitoring.MetricClient
	db                    *sql.DB
	earliestNotRecordedAt time.Time
	projectID             string
	pulsesNotRecorded     int
}

func NewGcpMonitor(ctx context.Context, db *sql.DB, gcpProjectID string) (*GcpMonitor, error) {
	gcpClient, err := monitoring.NewMetricClient(ctx)
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

	startTime := &timestamp.Timestamp{Seconds: g.earliestNotRecordedAt.Unix()}
	endTime := &timestamp.Timestamp{Seconds: recordedAt.Unix()}

	req := &monitoringpb.CreateTimeSeriesRequest{
		Name: "projects/" + g.projectID,
		TimeSeries: []*monitoringpb.TimeSeries{
			{
				Metric: &metricpb.Metric{
					Type: "custom.googleapis.com/test2",
					Labels: map[string]string{
						"environment": "STAGING",
					},
				},
				MetricKind: metric.MetricDescriptor_CUMULATIVE,
				Resource: &monitoredres.MonitoredResource{
					Type: "global",
					Labels: map[string]string{
						"project_id": g.projectID,
					},
				},
				Points: []*monitoringpb.Point{
					&monitoringpb.Point{
						Interval: &monitoringpb.TimeInterval{
							StartTime: startTime,
							EndTime:   endTime,
						},
						Value: &monitoringpb.TypedValue{
							Value: &monitoringpb.TypedValue_DoubleValue{
								DoubleValue: 0.1 * float64(g.pulsesNotRecorded),
							},
						},
					},
				},
			},
		},
	}
	// should kill this when we don't care anymore.
	log.Printf("writeTimeseriesRequest: %+v\n", req)

	err := g.gcpClient.CreateTimeSeries(g.ctx, req)
	if err != nil {
		return fmt.Errorf("could not write time series value, %v ", err)
	}

	g.earliestNotRecordedAt = time.Time{}
	g.pulsesNotRecorded = 0

	return nil
}
