package pulselisteners

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/golang/protobuf/ptypes/timestamp"
	"google.golang.org/genproto/googleapis/api/monitoredres"

	monitoring "cloud.google.com/go/monitoring/apiv3"
	timestamp "github.com/golang/protobuf/ptypes/timestamp"
	metricpb "google.golang.org/genproto/googleapis/api/metric"
	monitoredres "google.golang.org/genproto/googleapis/api/monitoredres"
	monitoringpb "google.golang.org/genproto/googleapis/monitoring/v3"
)

type GcpMonitor struct {
	db        *sql.DB
	projectID string
}

func NewGcpMonitor(db *sql.DB) *GcpMonitor {
	(&GcpMonitor{
		db: db,
	}).writeTimeSeriesValue()

	return &GcpMonitor{
		db: db,
	}
}

func (g *GcpMonitor) HandlePulse(recordedAt time.Time) error {
	/*
		if _, err := g.db.Exec("insert into meter (recorded_at) values ($1)", recordedAt); err != nil {
			log.Printf("error inserting into db, continuing. %s\n", err.Error())
			return err
		}
	*/
	return nil
}

// writeTimeSeriesValue writes a value for the custom metric created
func (g *GcpMonitor) writeTimeSeriesValue() error {
	ctx := context.Background()
	c, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		return err
	}
	now := &timestamp.Timestamp{
		Seconds: time.Now().Unix(),
	}
	req := &monitoringpb.CreateTimeSeriesRequest{
		Name: "projects/" + g.projectID,
		TimeSeries: []*monitoringpb.TimeSeries{{Metric: &metricpb.Metric{
			Type: "custom.googleapis.com/custom_measurement",
			Labels: map[string]string{
				"environment": "STAGING",
			},
		},
			Resource: &monitoredres.MonitoredResource{
				Type: "gce_instance",
				Labels: map[string]string{
					"instance_id": "test-instance",
					"zone":        "us-central1-f",
				},
			},
			Points: []*monitoringpb.Point{{Interval: &monitoringpb.TimeInterval{
				StartTime: now,
				EndTime:   now,
			},
				Value: &monitoringpb.TypedValue{
					Value: &monitoringpb.TypedValue_Int64Value{
						Int64Value: rand.Int63n(10),
					},
				},
			}},
		}},
	}
	log.Printf("writeTimeseriesRequest: %+v\n", req)

	err = c.CreateTimeSeries(ctx, req)
	if err != nil {
		return fmt.Errorf("could not write time series value, %v ", err)
	}
	return nil
}
