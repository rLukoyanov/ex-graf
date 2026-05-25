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
