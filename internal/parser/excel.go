package parser

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"influx/internal/model"
)

type ParseResult struct {
	Headers []string
	Columns []model.ColumnInfo
	Data    []map[string]interface{}
}

func ParseExcel(reader io.Reader) (*ParseResult, error) {
	f, err := excelize.OpenReader(reader)
	if err != nil {
		return nil, fmt.Errorf("open excel file: %w", err)
	}
	defer f.Close()

	sheetName := f.GetSheetName(0)
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("read sheet %s: %w", sheetName, err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("file must have at least a header row and one data row")
	}

	headers := rows[0]
	if len(headers) == 0 {
		return nil, fmt.Errorf("header row is empty")
	}

	columns := detectColumnTypes(rows, headers)

	var data []map[string]interface{}
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		record := make(map[string]interface{})
		hasData := false
		for j, h := range headers {
			var val string
			if j < len(row) {
				val = strings.TrimSpace(row[j])
			}
			if val == "" {
				continue
			}
			hasData = true
			if columns[j].Type == "number" {
				if f, err := strconv.ParseFloat(val, 64); err == nil {
					record[h] = f
				} else {
					record[h] = val
				}
			} else {
				record[h] = val
			}
		}
		if hasData {
			data = append(data, record)
		}
	}

	return &ParseResult{
		Headers: headers,
		Columns: columns,
		Data:    data,
	}, nil
}

func detectColumnTypes(rows [][]string, headers []string) []model.ColumnInfo {
	columns := make([]model.ColumnInfo, len(headers))
	for j, h := range headers {
		col := model.ColumnInfo{Name: h, Type: "string"}
		for i := 1; i < len(rows); i++ {
			var val string
			if j < len(rows[i]) {
				val = strings.TrimSpace(rows[i][j])
			}
			if val == "" {
				continue
			}
			if _, err := strconv.ParseFloat(val, 64); err == nil {
				col.Type = "number"
				break
			}
		}
		columns[j] = col
	}
	return columns
}
