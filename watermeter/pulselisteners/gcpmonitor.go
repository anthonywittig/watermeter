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
	db        *sql.DB
	projectID string
	recordedAtBuffer []time.Time
}

func NewGcpMonitor(db *sql.DB, gcpProjectID string) *GcpMonitor {
	return &GcpMonitor{
		db:        db,
		projectID: gcpProjectID,
		recordedAtBuffer: []time.Time{},
	}
}

func (g *GcpMonitor) HandlePulse(recordedAt time.Time) error {
	g.recordedAtBuffer = append(g.recordedAtBuffer, recordedAt)
	if len(g.recordedAtBuffer) < 4 {
		return nil
	}

	ctx := context.Background()
	c, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		return err
	}

	startTime := &timestamp.Timestamp{Seconds: g.recordedAtBuffer[0].Unix()}
	endTime := &timestamp.Timestamp{Seconds: g.recordedAtBuffer[len(g.recordedAtBuffer) - 1].Unix()}

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
								DoubleValue: 0.1 * float64(len(g.recordedAtBuffer)),
							},
						},
					},
				},

			},
		},
	}
	log.Printf("writeTimeseriesRequest: %+v\n", req)

	err = c.CreateTimeSeries(ctx, req)
	if err != nil {
		return fmt.Errorf("could not write time series value, %v ", err)
	}

	g.recordedAtBuffer = []time.Time{}

	return nil
}
