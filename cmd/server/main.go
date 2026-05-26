package main

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"influx/internal/handler"
	"influx/internal/store"
)

func main() {
	influxURL := getEnv("INFLUX_URL", "http://localhost:8086")
	influxToken := getEnv("INFLUX_TOKEN", "my-super-secret-token")
	influxOrg := getEnv("INFLUX_ORG", "influx")
	influxBucket := getEnv("INFLUX_BUCKET", "excel")
	serverPort := getEnv("SERVER_PORT", "8080")

	s := store.New(influxURL, influxToken, influxOrg, influxBucket)
	defer s.Close()

	tmpl := template.Must(template.ParseGlob("web/templates/*.html"))

	pagesHandler := handler.NewPagesHandler(s, tmpl)
	uploadHandler := handler.NewUploadHandler(s, tmpl)
	compareHandler := handler.NewCompareHandler(s, tmpl)

	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)

	r.Get("/", pagesHandler.Index)
	r.Get("/compare", compareHandler.Compare)
	r.Post("/upload", uploadHandler.Upload)

	fileServer := http.FileServer(http.Dir("web/static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	srv := &http.Server{
		Addr:    ":" + serverPort,
		Handler: r,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("Starting server on :%s", serverPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-quit
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("forced shutdown: %v", err)
	}

	log.Println("Server stopped")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
