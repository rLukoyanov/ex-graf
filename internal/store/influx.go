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

	t := upload.CreatedAt

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
	}

	if err := s.saveDiff(ctx, upload, columns); err != nil {
		return fmt.Errorf("save diff: %w", err)
	}

	return nil
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

func (s *InfluxStore) saveDiff(ctx context.Context, upload model.Upload, columns []model.ColumnInfo) error {
	tagCols := make([]string, 0, len(columns))
	for _, c := range columns {
		if c.Type == "string" {
			tagCols = append(tagCols, c.Name)
		}
	}
	if len(tagCols) == 0 {
		return nil
	}

	curSets, err := s.getTagSets(ctx, upload.ID, tagCols)
	if err != nil {
		return fmt.Errorf("get current tag sets: %w", err)
	}

	prev, err := s.findPrevUpload(ctx, upload)
	if err != nil {
		return fmt.Errorf("find prev upload: %w", err)
	}

	t := upload.CreatedAt
	writeAPI := s.client.WriteAPI(s.org, s.bucket)
	errorsCh := writeAPI.Errors()

	if prev == nil {
		point := influxdb2.NewPointWithMeasurement("upload_diff").
			AddTag("upload_id", upload.ID).
			AddTag("filename", upload.Filename).
			AddField("added_count", 0).
			AddField("removed_count", 0).
			AddField("common_count", 0).
			AddField("start_total", 0).
			AddField("end_total", int64(len(curSets))).
			SetTime(t)
		writeAPI.WritePoint(point)
	} else {
		prevSets, err := s.getTagSets(ctx, prev.ID, tagCols)
		if err != nil {
			return fmt.Errorf("get prev tag sets: %w", err)
		}

		prevKeys := make(map[string]bool)
		for _, set := range prevSets {
			prevKeys[tagSetKey(set, tagCols)] = true
		}
		curKeys := make(map[string]bool)
		for _, set := range curSets {
			curKeys[tagSetKey(set, tagCols)] = true
		}

		var addedCount, removedCount, commonCount int64
		for _, set := range curSets {
			if prevKeys[tagSetKey(set, tagCols)] {
				commonCount++
			} else {
				addedCount++
			}
		}
		for _, set := range prevSets {
			if !curKeys[tagSetKey(set, tagCols)] {
				removedCount++
			}
		}

		point := influxdb2.NewPointWithMeasurement("upload_diff").
			AddTag("upload_id", upload.ID).
			AddTag("filename", upload.Filename).
			AddTag("prev_upload_id", prev.ID).
			AddField("added_count", addedCount).
			AddField("removed_count", removedCount).
			AddField("common_count", commonCount).
			AddField("start_total", int64(len(prevSets))).
			AddField("end_total", int64(len(curSets))).
			SetTime(t)
		writeAPI.WritePoint(point)
	}

	writeAPI.Flush()

	select {
	case err := <-errorsCh:
		return fmt.Errorf("write diff error: %w", err)
	default:
		return nil
	}
}

func (s *InfluxStore) findPrevUpload(ctx context.Context, upload model.Upload) (*model.Upload, error) {
	q := fmt.Sprintf(`from(bucket: "%s")
		|> range(start: -100y, stop: %s)
		|> filter(fn: (r) => r._measurement == "upload_meta" and r.upload_id != "%s")
		|> pivot(rowKey: ["_time"], columnKey: ["_field"], valueColumn: "_value")
		|> group()
		|> sort(columns: ["_time"], desc: true)
		|> limit(n: 1)`, s.bucket, upload.CreatedAt.Format(time.RFC3339Nano), upload.ID)

	result, err := s.client.QueryAPI(s.org).Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query prev upload: %w", err)
	}
	for result.Next() {
		rec := result.Record()
		rc, _ := rec.ValueByKey("record_count").(int64)
		colJSON, _ := rec.ValueByKey("columns_json").(string)
		return &model.Upload{
			ID:          rec.ValueByKey("upload_id").(string),
			Filename:    rec.ValueByKey("filename").(string),
			CreatedAt:   rec.Time(),
			RecordCount: rc,
			ColumnsJSON: colJSON,
		}, nil
	}
	if result.Err() != nil {
		return nil, fmt.Errorf("iterate prev upload: %w", result.Err())
	}
	return nil, nil
}

func (s *InfluxStore) findLatestUploadBefore(ctx context.Context, t time.Time) (*model.Upload, error) {
	q := fmt.Sprintf(`from(bucket: "%s")
		|> range(start: -100y, stop: %s)
		|> filter(fn: (r) => r._measurement == "upload_meta")
		|> pivot(rowKey: ["_time"], columnKey: ["_field"], valueColumn: "_value")
		|> group()
		|> sort(columns: ["_time"], desc: true)
		|> limit(n: 1)`, s.bucket, t.Format(time.RFC3339Nano))

	result, err := s.client.QueryAPI(s.org).Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query latest upload: %w", err)
	}

	for result.Next() {
		rec := result.Record()
		rc, _ := rec.ValueByKey("record_count").(int64)
		colJSON, _ := rec.ValueByKey("columns_json").(string)
		return &model.Upload{
			ID:          rec.ValueByKey("upload_id").(string),
			Filename:    rec.ValueByKey("filename").(string),
			CreatedAt:   rec.Time(),
			RecordCount: rc,
			ColumnsJSON: colJSON,
		}, nil
	}
	if result.Err() != nil {
		return nil, fmt.Errorf("iterate latest upload: %w", result.Err())
	}
	return nil, nil
}

func (s *InfluxStore) listTagColumns(ctx context.Context, uploadID string) ([]string, error) {
	q := fmt.Sprintf(`from(bucket: "%s")
		|> range(start: -100y)
		|> filter(fn: (r) => r._measurement == "upload_meta" and r.upload_id == "%s")
		|> pivot(rowKey: ["_time"], columnKey: ["_field"], valueColumn: "_value")
		|> limit(n: 1)`, s.bucket, uploadID)

	result, err := s.client.QueryAPI(s.org).Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query columns: %w", err)
	}
	for result.Next() {
		rec := result.Record()
		colJSON, _ := rec.ValueByKey("columns_json").(string)
		if colJSON == "" {
			continue
		}
		var columns []model.ColumnInfo
		if err := json.Unmarshal([]byte(colJSON), &columns); err != nil {
			return nil, fmt.Errorf("unmarshal columns: %w", err)
		}
		var tagCols []string
		for _, c := range columns {
			if c.Type == "string" {
				tagCols = append(tagCols, c.Name)
			}
		}
		return tagCols, nil
	}
	if result.Err() != nil {
		return nil, fmt.Errorf("iterate columns: %w", result.Err())
	}
	return nil, nil
}

func (s *InfluxStore) getTagSets(ctx context.Context, uploadID string, tagColumns []string) ([]map[string]string, error) {
	q := fmt.Sprintf(`from(bucket: "%s")
		|> range(start: -100y)
		|> filter(fn: (r) => r._measurement == "excel_data" and r.upload_id == "%s")
		|> pivot(rowKey: ["_time"], columnKey: ["_field"], valueColumn: "_value")
		|> group()`, s.bucket, uploadID)

	result, err := s.client.QueryAPI(s.org).Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query records: %w", err)
	}

	seen := make(map[string]bool)
	var sets []map[string]string

	for result.Next() {
		rec := result.Record()
		tagSet := make(map[string]string)
		var keyParts []string
		for _, col := range tagColumns {
			val := rec.ValueByKey(col)
			if val == nil {
				continue
			}
			if s, ok := val.(string); ok && s != "" {
				tagSet[col] = s
				keyParts = append(keyParts, col+"="+s)
			}
		}
		if len(keyParts) == 0 {
			continue
		}
		key := ""
		for i, part := range keyParts {
			if i > 0 {
				key += "|"
			}
			key += part
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		sets = append(sets, tagSet)
	}
	if result.Err() != nil {
		return nil, fmt.Errorf("iterate records: %w", result.Err())
	}
	return sets, nil
}

func (s *InfluxStore) ComparePeriods(ctx context.Context, start, end time.Time) (*model.CompareResult, error) {
	startUpload, err := s.findLatestUploadBefore(ctx, start)
	if err != nil {
		return nil, fmt.Errorf("find start upload: %w", err)
	}
	endUpload, err := s.findLatestUploadBefore(ctx, end)
	if err != nil {
		return nil, fmt.Errorf("find end upload: %w", err)
	}

	res := &model.CompareResult{
		StartDate: start.Format("02.01.2006"),
		EndDate:   end.Format("02.01.2006"),
	}

	if startUpload != nil {
		res.StartID = startUpload.ID
	}
	if endUpload != nil {
		res.EndID = endUpload.ID
	}

	if startUpload == nil && endUpload == nil {
		return res, nil
	}

	if startUpload != nil && endUpload != nil && startUpload.ID == endUpload.ID {
		tagCols, _ := s.listTagColumns(ctx, startUpload.ID)
		sets, _ := s.getTagSets(ctx, startUpload.ID, tagCols)
		res.StartTotal = len(sets)
		res.EndTotal = len(sets)
		res.Common = len(sets)
		return res, nil
	}

	allCols := make(map[string]bool)
	var startCols, endCols []string

	if startUpload != nil {
		startCols, _ = s.listTagColumns(ctx, startUpload.ID)
		for _, c := range startCols {
			allCols[c] = true
		}
	}
	if endUpload != nil {
		endCols, _ = s.listTagColumns(ctx, endUpload.ID)
		for _, c := range endCols {
			allCols[c] = true
		}
	}

	unionCols := make([]string, 0, len(allCols))
	for c := range allCols {
		unionCols = append(unionCols, c)
	}

	var startSets, endSets []map[string]string
	if startUpload != nil {
		startSets, _ = s.getTagSets(ctx, startUpload.ID, startCols)
		res.StartTotal = len(startSets)
	}
	if endUpload != nil {
		endSets, _ = s.getTagSets(ctx, endUpload.ID, endCols)
		res.EndTotal = len(endSets)
	}

	startKeys := make(map[string]bool)
	for _, set := range startSets {
		startKeys[tagSetKey(set, unionCols)] = true
	}
	endKeys := make(map[string]bool)
	for _, set := range endSets {
		endKeys[tagSetKey(set, unionCols)] = true
	}

	var added, removed []map[string]string
	for _, set := range endSets {
		if !startKeys[tagSetKey(set, unionCols)] {
			added = append(added, set)
		}
	}
	for _, set := range startSets {
		if !endKeys[tagSetKey(set, unionCols)] {
			removed = append(removed, set)
		}
	}

	res.Added = toCompareRecords(added, unionCols)
	res.Removed = toCompareRecords(removed, unionCols)
	res.Common = len(endSets) - len(added)

	return res, nil
}

func tagSetKey(tagSet map[string]string, tagCols []string) string {
	var key string
	for _, col := range tagCols {
		key += "|" + tagSet[col]
	}
	return key
}

func toCompareRecords(sets []map[string]string, tagCols []string) []model.CompareRecord {
	records := make([]model.CompareRecord, len(sets))
	for i, s := range sets {
		r := model.CompareRecord{Tags: make(map[string]string)}
		for _, col := range tagCols {
			if v, ok := s[col]; ok {
				r.Tags[col] = v
			}
		}
		records[i] = r
	}
	return records
}

func (s *InfluxStore) QueryAPI() api.QueryAPI {
	return s.client.QueryAPI(s.org)
}
