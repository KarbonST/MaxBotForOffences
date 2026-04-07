package reporting

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"max_bot/internal/reference"
)

func NewHandler(service *Service, refs reference.Provider, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/api/bot/reference/categories", func(w http.ResponseWriter, r *http.Request) {
		serveReferenceList(w, r, logger, refs.Categories)
	})
	mux.HandleFunc("/api/bot/reference/municipalities", func(w http.ResponseWriter, r *http.Request) {
		serveReferenceList(w, r, logger, refs.Municipalities)
	})
	mux.HandleFunc("/api/bot/reports", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handleCreateReport(w, r, logger, service)
		case http.MethodGet:
			handleListReports(w, r, logger, service)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/bot/reports/by-user/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		raw := strings.TrimPrefix(r.URL.Path, "/api/bot/reports/by-user/")
		maxUserID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || maxUserID <= 0 {
			http.Error(w, "invalid max user id", http.StatusBadRequest)
			return
		}
		items, err := service.ListReportsByMaxUserID(r.Context(), maxUserID)
		if err != nil {
			logger.Error("list reports by user failed", "max_user_id", maxUserID, "error", err.Error())
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	})
	mux.HandleFunc("/api/bot/reports/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		raw := strings.TrimPrefix(r.URL.Path, "/api/bot/reports/")
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "invalid report id", http.StatusBadRequest)
			return
		}
		item, err := service.GetReportByID(r.Context(), id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				http.Error(w, "report not found", http.StatusNotFound)
				return
			}
			logger.Error("get report failed", "id", id, "error", err.Error())
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("/api/bot/conversations/", func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.URL.Path, "/api/bot/conversations/")
		maxUserID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || maxUserID <= 0 {
			http.Error(w, "invalid max user id", http.StatusBadRequest)
			return
		}

		switch r.Method {
		case http.MethodGet:
			item, err := service.GetConversation(r.Context(), maxUserID)
			if err != nil {
				if errors.Is(err, ErrInvalidRequest) {
					http.Error(w, "invalid max user id", http.StatusBadRequest)
					return
				}
				logger.Error("get conversation failed", "max_user_id", maxUserID, "error", err.Error())
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
				return
			}
			writeJSON(w, http.StatusOK, item)
		case http.MethodPut:
			var req SaveConversationRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			req.MaxUserID = maxUserID
			item, err := service.SaveConversation(r.Context(), req)
			if err != nil {
				if errors.Is(err, ErrInvalidRequest) {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
					return
				}
				logger.Error("save conversation failed", "max_user_id", maxUserID, "error", err.Error())
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
				return
			}
			writeJSON(w, http.StatusOK, item)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/bot/notifications/pending", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		limit := parseIntDefault(r.URL.Query().Get("limit"), 50)
		items, err := service.ListPendingNotifications(r.Context(), limit)
		if err != nil {
			logger.Error("list pending notifications failed", "error", err.Error())
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	})
	mux.HandleFunc("/api/bot/notifications/", func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.URL.Path, "/api/bot/notifications/")
		parts := strings.Split(strings.Trim(raw, "/"), "/")
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}

		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "invalid notification id", http.StatusBadRequest)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		switch parts[1] {
		case "sent":
			err = service.MarkNotificationSent(r.Context(), id)
		case "error":
			err = service.MarkNotificationError(r.Context(), id)
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			if errors.Is(err, ErrInvalidRequest) {
				http.Error(w, "invalid notification id", http.StatusBadRequest)
				return
			}
			if errors.Is(err, ErrNotFound) {
				http.Error(w, "notification not found", http.StatusNotFound)
				return
			}
			logger.Error("update notification status failed", "notification_id", id, "action", parts[1], "error", err.Error())
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/api/bot/clarifications/pending/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		raw := strings.TrimPrefix(r.URL.Path, "/api/bot/clarifications/pending/")
		maxUserID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || maxUserID <= 0 {
			http.Error(w, "invalid max user id", http.StatusBadRequest)
			return
		}

		item, err := service.GetPendingClarification(r.Context(), maxUserID)
		if err != nil {
			if errors.Is(err, ErrInvalidRequest) {
				http.Error(w, "invalid max user id", http.StatusBadRequest)
				return
			}
			if errors.Is(err, ErrNotFound) {
				http.Error(w, "clarification not found", http.StatusNotFound)
				return
			}
			logger.Error("get pending clarification failed", "max_user_id", maxUserID, "error", err.Error())
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		writeJSON(w, http.StatusOK, item)
	})
	mux.HandleFunc("/api/bot/clarifications/", func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.URL.Path, "/api/bot/clarifications/")
		parts := strings.Split(strings.Trim(raw, "/"), "/")
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}

		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "invalid clarification id", http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		switch parts[1] {
		case "answer":
			var req ClarificationAnswerRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			req.ClarificationID = id
			if err := service.AnswerClarification(r.Context(), req); err != nil {
				handleClarificationActionError(w, logger, "answer clarification", id, err)
				return
			}
		case "reject":
			var req ClarificationRejectRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			req.ClarificationID = id
			if err := service.RejectClarification(r.Context(), req); err != nil {
				handleClarificationActionError(w, logger, "reject clarification", id, err)
				return
			}
		default:
			http.NotFound(w, r)
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return mux
}

func handleCreateReport(w http.ResponseWriter, r *http.Request, logger *slog.Logger, service *Service) {
	var req CreateReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	result, err := service.CreateReport(r.Context(), req)
	if err != nil {
		if errors.Is(err, ErrInvalidRequest) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		logger.Error("create report failed", "error", err.Error())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusCreated, result)
}

func handleListReports(w http.ResponseWriter, r *http.Request, logger *slog.Logger, service *Service) {
	filter := ListReportsFilter{
		Status:         r.URL.Query().Get("status"),
		Search:         r.URL.Query().Get("q"),
		CategoryID:     parseIntDefault(r.URL.Query().Get("category_id"), 0),
		MunicipalityID: parseIntDefault(r.URL.Query().Get("municipality_id"), 0),
		Limit:          parseIntDefault(r.URL.Query().Get("limit"), 50),
		Offset:         parseIntDefault(r.URL.Query().Get("offset"), 0),
	}
	if raw := r.URL.Query().Get("max_user_id"); raw != "" {
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil && value > 0 {
			filter.MaxUserID = &value
		}
	}

	items, err := service.ListReports(r.Context(), filter)
	if err != nil {
		logger.Error("list reports failed", "error", err.Error())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func serveReferenceList(w http.ResponseWriter, r *http.Request, logger *slog.Logger, loader func(context.Context) ([]reference.Item, error)) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	items, err := loader(r.Context())
	if err != nil {
		logger.Error("reference lookup failed", "error", err.Error())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func handleClarificationActionError(w http.ResponseWriter, logger *slog.Logger, action string, clarificationID int64, err error) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, ErrNotFound):
		http.Error(w, "clarification not found", http.StatusNotFound)
	default:
		logger.Error(action+" failed", "clarification_id", clarificationID, "error", err.Error())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func parseIntDefault(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}
