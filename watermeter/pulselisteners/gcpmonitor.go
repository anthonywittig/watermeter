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
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

type GcpMonitor struct {
	db        *sql.DB
	projectID string
}

func NewGcpMonitor(db *sql.DB, gcpProjectID string) *GcpMonitor {
	return &GcpMonitor{
		db:        db,
		projectID: gcpProjectID,
	}
}

func (g *GcpMonitor) HandlePulse(recordedAt time.Time) error {
	ctx := context.Background()
	c, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		return err
	}

	recordedAtTimestamp := &timestamp.Timestamp{
		Seconds: recordedAt.Unix(),
	}

	req := &monitoringpb.CreateTimeSeriesRequest{
		Name: "projects/" + g.projectID,
		TimeSeries: []*monitoringpb.TimeSeries{
			{
				Metric: &metricpb.Metric{
					Type: "github.com/anthonywittig/watermeter/flow",
					Labels: map[string]string{
						"environment": "STAGING",
					},
				},
				MetricKind: metric.MetricDescriptor_GAUGE,
				/*
					Resource: &monitoredres.MonitoredResource{
						Type: "location",
						Labels: map[string]string{
							"name": "home",
						},
					},
				*/
				Points: []*monitoringpb.Point{
					{
						Interval: &monitoringpb.TimeInterval{
							StartTime: recordedAtTimestamp,
							EndTime:   recordedAtTimestamp,
						},
						Value: &monitoringpb.TypedValue{
							Value: &monitoringpb.TypedValue_DoubleValue{
								DoubleValue: 0.1,
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
	return nil
}
