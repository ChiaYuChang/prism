package rss

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

func Mux(logger *slog.Logger) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		m := map[string]any{}
		m["status"] = "OK"
		m["time"] = time.Now().Format(time.RFC3339)
		w.WriteHeader(http.StatusOK)
		b, _ := json.Marshal(m)
		if _, err := w.Write(b); err != nil {
			logger.Error("failed to write health response", "error", err)
		}
	})

	mux.HandleFunc("/yahoo/{category}", func(w http.ResponseWriter, r *http.Request) {
		category := r.PathValue("category")
		level := new(slog.Level)
		status := new(int)
		length := new(int)
		start := time.Now()

		defer func() {
			logger.Log(
				context.Background(),
				*level,
				"hit yahoo endpoint",
				"category", category,
				"status", *status,
				"length", *length,
				"time", time.Since(start).Milliseconds(),
			)
		}()

		if category == "" {
			m := map[string]any{}
			m["status"] = "error"
			m["message"] = "category is required"
			b, _ := json.Marshal(m)
			w.WriteHeader(http.StatusBadRequest)
			if _, err := w.Write(b); err != nil {
				logger.Error("failed to write yahoo error response", "category", category, "error", err)
			}
			*status = http.StatusBadRequest
			*length = len(b)
			*level = slog.LevelError
			return
		}

		rss, err := YahooNews(category)
		if err != nil {
			m := map[string]any{}
			m["status"] = "error"
			m["message"] = err.Error()
			b, _ := json.Marshal(m)
			w.WriteHeader(http.StatusInternalServerError)
			if _, err := w.Write(b); err != nil {
				logger.Error("failed to write yahoo error response", "category", category, "error", err)
			}
			*status = http.StatusInternalServerError
			*length = len(b)
			*level = slog.LevelError
			return
		}
		*status = http.StatusOK
		*length = len(rss)
		*level = slog.LevelInfo
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(rss)); err != nil {
			logger.Error("failed to write yahoo rss response", "category", category, "error", err)
		}
	})

	return mux
}
