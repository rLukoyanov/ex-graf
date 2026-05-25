package handler

import (
	"html/template"
	"net/http"

	"influx/internal/store"
)

type PagesHandler struct {
	store *store.InfluxStore
	tmpl  *template.Template
}

func NewPagesHandler(s *store.InfluxStore, tmpl *template.Template) *PagesHandler {
	return &PagesHandler{store: s, tmpl: tmpl}
}

func (h *PagesHandler) Index(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.GetStats(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Stats": stats,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "base.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
