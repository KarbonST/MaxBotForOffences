package reference

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
)

func NewHandler(provider Provider, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/api/bot/reference/categories", func(w http.ResponseWriter, r *http.Request) {
		serveList(w, r, logger, "categories", provider.Categories)
	})
	mux.HandleFunc("/api/bot/reference/municipalities", func(w http.ResponseWriter, r *http.Request) {
		serveList(w, r, logger, "municipalities", provider.Municipalities)
	})

	return mux
}

func serveList(w http.ResponseWriter, r *http.Request, logger *slog.Logger, kind string, loader func(context.Context) ([]Item, error)) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	items, err := loader(r.Context())
	if err != nil {
		logger.Error("reference lookup failed", "kind", kind, "error", err.Error())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, listResponse{Items: items})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
