package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"influx/internal/model"
	"influx/internal/parser"
	"influx/internal/store"
)

const maxUploadSize = 50 << 20

type UploadHandler struct {
	store *store.InfluxStore
	tmpl  *template.Template
}

func NewUploadHandler(s *store.InfluxStore, tmpl *template.Template) *UploadHandler {
	return &UploadHandler{store: s, tmpl: tmpl}
}

func (h *UploadHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		http.Error(w, fmt.Sprintf("parse form: %v", err), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, fmt.Sprintf("get file: %v", err), http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, fmt.Sprintf("read file: %v", err), http.StatusInternalServerError)
		return
	}

	parseResult, err := parser.ParseExcel(noopCloseReader{bytes.NewReader(data)})
	if err != nil {
		http.Error(w, fmt.Sprintf("parse excel: %v", err), http.StatusBadRequest)
		return
	}

	upload := model.Upload{
		ID:          uuid.New().String(),
		Filename:    header.Filename,
		CreatedAt:   time.Now(),
		RecordCount: int64(len(parseResult.Data)),
	}

	colJSON, _ := json.Marshal(parseResult.Columns)
	upload.ColumnsJSON = string(colJSON)

	if err := h.store.SaveUpload(r.Context(), upload, parseResult.Columns, parseResult.Data); err != nil {
		http.Error(w, fmt.Sprintf("save upload: %v", err), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

type noopCloseReader struct {
	reader io.Reader
}

func (n noopCloseReader) Read(p []byte) (int, error) {
	return n.reader.Read(p)
}

func (n noopCloseReader) Close() error {
	return nil
}
