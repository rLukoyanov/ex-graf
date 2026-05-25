package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"

	"influx/internal/model"
)

type InfluxStore struct {
	client influxdb2.Client
	org    string
	bucket string
}

func New(url, token, org, bucket string) *InfluxStore {
	client := influxdb2.NewClient(url, token)
	return &InfluxStore{client: client, org: org, bucket: bucket}
}

func (s *InfluxStore) Close() {
	s.client.Close()
}

func (s *InfluxStore) SaveUpload(ctx context.Context, upload model.Upload, columns []model.ColumnInfo, records []map[string]interface{}) error {
	writeAPI := s.client.WriteAPI(s.org, s.bucket)
	errorsCh := writeAPI.Errors()

	t := time.Now()

	for _, rec := range records {
		point := influxdb2.NewPointWithMeasurement("excel_data").
			AddTag("upload_id", upload.ID).
			AddTag("filename", upload.Filename).
			SetTime(t)

		point.AddField("_record_count", int64(1))

		for _, col := range columns {
			val, ok := rec[col.Name]
			if !ok || val == nil {
				continue
			}
			switch col.Type {
			case "number":
				if f, ok := val.(float64); ok {
					point.AddField(col.Name, f)
				} else if s, ok := val.(string); ok {
					point.AddTag(col.Name, s)
				}
			default:
				if s, ok := val.(string); ok {
					point.AddTag(col.Name, s)
				}
			}
		}

		writeAPI.WritePoint(point)
	}

	colJSON, err := json.Marshal(columns)
	if err != nil {
		return fmt.Errorf("marshal columns: %w", err)
	}

	meta := influxdb2.NewPointWithMeasurement("upload_meta").
		AddTag("upload_id", upload.ID).
		AddTag("filename", upload.Filename).
		AddField("record_count", upload.RecordCount).
		AddField("columns_json", string(colJSON)).
		SetTime(t)

	writeAPI.WritePoint(meta)
	writeAPI.Flush()

	select {
	case err := <-errorsCh:
		return fmt.Errorf("write error: %w", err)
	default:
		return nil
	}
}

func (s *InfluxStore) GetStats(ctx context.Context) (*model.Stats, error) {
	stats := &model.Stats{}

	q := fmt.Sprintf(`from(bucket: "%s")
		|> range(start: -100y)
		|> filter(fn: (r) => r._measurement == "upload_meta")
		|> pivot(rowKey: ["_time"], columnKey: ["_field"], valueColumn: "_value")
		|> group()
		|> sort(columns: ["_time"], desc: true)`, s.bucket)

	result, err := s.client.QueryAPI(s.org).Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query uploads: %w", err)
	}

	var uploads []model.Upload
	for result.Next() {
		rec := result.Record()
		stats.UploadCount++

		rc, _ := rec.ValueByKey("record_count").(int64)
		colJSON, _ := rec.ValueByKey("columns_json").(string)

		upload := model.Upload{
			ID:          rec.ValueByKey("upload_id").(string),
			Filename:    rec.ValueByKey("filename").(string),
			CreatedAt:   rec.Time(),
			RecordCount: rc,
			ColumnsJSON: colJSON,
		}
		uploads = append(uploads, upload)
	}
	if result.Err() != nil {
		return nil, fmt.Errorf("iterate uploads: %w", result.Err())
	}
	stats.Uploads = uploads

	totalQ := fmt.Sprintf(`from(bucket: "%s")
		|> range(start: -100y)
		|> filter(fn: (r) => r._measurement == "excel_data" and r._field == "_record_count")
		|> group()
		|> sum()`, s.bucket)

	totalResult, err := s.client.QueryAPI(s.org).Query(ctx, totalQ)
	if err != nil {
		return nil, fmt.Errorf("query total records: %w", err)
	}
	if totalResult.Next() {
		stats.TotalRecords, _ = totalResult.Record().Value().(int64)
	}
	if totalResult.Err() != nil {
		return nil, fmt.Errorf("total records: %w", totalResult.Err())
	}

	return stats, nil
}

func (s *InfluxStore) QueryAPI() api.QueryAPI {
	return s.client.QueryAPI(s.org)
}
