package handler

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
	"time"

	"influx/internal/store"
)

type CompareHandler struct {
	store *store.InfluxStore
	tmpl  *template.Template
}

func NewCompareHandler(s *store.InfluxStore, tmpl *template.Template) *CompareHandler {
	return &CompareHandler{store: s, tmpl: tmpl}
}

func (h *CompareHandler) Compare(w http.ResponseWriter, r *http.Request) {
	start := r.URL.Query().Get("start")
	end := r.URL.Query().Get("end")

	var startTime, endTime time.Time

	if start != "" {
		t, err := time.Parse("2006-01-02", start)
		if err != nil {
			http.Error(w, "invalid start date (use YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		startTime = t
	}
	if end != "" {
		t, err := time.Parse("2006-01-02", end)
		if err != nil {
			http.Error(w, "invalid end date (use YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		endTime = t
	}

	if startTime.IsZero() || endTime.IsZero() {
		http.Error(w, "start and end query params required (YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	result, err := h.store.ComparePeriods(r.Context(), startTime, endTime)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/json") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(result)
		return
	}

	data := map[string]interface{}{
		"Result": result,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "base.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
