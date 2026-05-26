package model

import "time"

type ColumnInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type Upload struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	CreatedAt   time.Time `json:"created_at"`
	RecordCount int64     `json:"record_count"`
	ColumnsJSON string    `json:"columns_json"`
}

type Stats struct {
	TotalRecords int64    `json:"total_records"`
	UploadCount  int64    `json:"upload_count"`
	Uploads      []Upload `json:"uploads"`
}

type CompareRecord struct {
	Tags map[string]string `json:"tags"`
}

type CompareResult struct {
	StartID    string           `json:"start_id"`
	EndID      string           `json:"end_id"`
	StartDate  string           `json:"start_date"`
	EndDate    string           `json:"end_date"`
	StartTotal int              `json:"start_total"`
	EndTotal   int              `json:"end_total"`
	Added      []CompareRecord  `json:"added"`
	Removed    []CompareRecord  `json:"removed"`
	Common     int              `json:"common"`
}
