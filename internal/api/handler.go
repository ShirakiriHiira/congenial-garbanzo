// Package api exposes the authenticated Mini App JSON API and health probes.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"telegram.bot.remind/internal/auth"
	"telegram.bot.remind/internal/service"
)

// Config carries the HTTP boundary dependencies. Ready reports process
// readiness; Ping probes the database and must honor the supplied context.
// A nil Now uses time.Now. AuthMaxAge defaults to five minutes when unset.
type Config struct {
	BotToken   string
	AuthMaxAge time.Duration
	Ready      func() bool
	Ping       func(context.Context) error
	Now        func() time.Time
}

// Handler implements http.Handler over the application service.
type Handler struct {
	service   *service.Service
	config    Config
	mux       *http.ServeMux
	fallbacks map[string]bool
}

// New builds the API. It never starts listeners or background work.
func New(s *service.Service, cfg Config) *Handler {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.AuthMaxAge <= 0 {
		cfg.AuthMaxAge = 5 * time.Minute
	}
	h := &Handler{service: s, config: cfg, mux: http.NewServeMux(), fallbacks: map[string]bool{}}

	h.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	h.mux.HandleFunc("/healthz", methodNotAllowed)
	h.mux.HandleFunc("GET /readyz", h.ready)
	h.mux.HandleFunc("/readyz", methodNotAllowed)

	h.mux.Handle("/api/v1/", h.authenticate(http.StripPrefix("/api/v1", h.apiMux())))
	h.mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		fail(w, http.StatusNotFound, "not_found")
	})
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// apiMux is built per Handler and retains route registrations.
func (h *Handler) apiMux() *http.ServeMux {
	m := http.NewServeMux()
	add := func(pattern string, handler http.HandlerFunc) {
		m.HandleFunc(pattern, handler)
		path := strings.SplitN(pattern, " ", 2)[1]
		if !h.fallbacks[path] {
			h.fallbacks[path] = true
			m.HandleFunc(path, methodNotAllowed)
		}
	}

	add("GET /me", h.getMe)
	add("PATCH /me", h.patchMe)
	add("GET /categories", h.listCategories)
	add("POST /categories", h.createCategory)
	add("PATCH /categories/{id}", h.patchCategory)
	// The literal overview path overlaps the more general item pattern. Register
	// every method /services/{id} serves so the literal path stays strictly more
	// specific, and skip the generic no-method fallback that would overlap it.
	m.HandleFunc("GET /services/overview", h.serviceOverview)
	m.HandleFunc("POST /services/overview", methodNotAllowed)
	m.HandleFunc("PATCH /services/overview", methodNotAllowed)
	m.HandleFunc("DELETE /services/overview", methodNotAllowed)
	add("GET /services", h.listServices)
	add("POST /services", h.createService)
	add("GET /services/{id}", h.getService)
	add("PATCH /services/{id}", h.patchService)
	add("DELETE /services/{id}", h.deleteService)
	add("GET /services/{id}/periods", h.listServicePeriods)
	add("POST /services/{id}/periods", h.createServicePeriod)

	add("GET /periods/{id}", h.getPeriod)
	add("PATCH /periods/{id}", h.patchPeriod)
	add("DELETE /periods/{id}", h.deletePeriod)
	add("GET /periods/{id}/rules", h.listRules)
	add("POST /periods/{id}/rules", h.createRule)
	add("GET /periods/{id}/renewals", h.listRenewals)
	add("POST /periods/{id}/renewals", h.createRenewal)

	add("GET /rules/{id}", h.getRule)
	add("PATCH /rules/{id}", h.patchRule)
	add("DELETE /rules/{id}", h.deleteRule)

	add("GET /reminders", h.listReminders)
	add("GET /reminders/{id}", h.getReminder)
	add("POST /reminders/{id}/cancel", h.cancelReminder)

	m.HandleFunc("/", notFound)
	return m
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	fail(w, http.StatusNotFound, "not_found")
}

func methodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	fail(w, http.StatusMethodNotAllowed, "method_not_allowed")
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	if h.config.Ready == nil || !h.config.Ready() || h.config.Ping == nil {
		fail(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if h.config.Ping(ctx) != nil {
		fail(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type userContextKey struct{}

func currentUser(r *http.Request) service.User {
	u, _ := r.Context().Value(userContextKey{}).(service.User)
	return u
}

func (h *Handler) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		raw, ok := authorization(r)
		if !ok {
			fail(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		telegramID, err := auth.VerifyInitData(raw, h.config.BotToken, h.config.Now(), h.config.AuthMaxAge)
		if err != nil {
			fail(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if !h.service.Allowed(telegramID) {
			fail(w, http.StatusForbidden, "forbidden")
			return
		}
		u, err := h.service.Authenticate(r.Context(), telegramID, nil)
		if err != nil {
			serviceError(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, u)))
	})
}

func authorization(r *http.Request) (string, bool) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return "", false
	}
	raw, found := strings.CutPrefix(values[0], "tma ")
	return raw, found && raw != ""
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func fail(w http.ResponseWriter, status int, code string) {
	messages := map[string]string{
		"invalid_input":          "Invalid request.",
		"unauthorized":           "Invalid or expired authentication.",
		"forbidden":              "Access denied.",
		"not_found":              "Resource not found.",
		"conflict":               "The resource has changed or the operation conflicts with its state.",
		"unavailable":            "Service temporarily unavailable.",
		"unsupported_media_type": "Content-Type must be application/json.",
		"payload_too_large":      "Request body is too large.",
		"method_not_allowed":     "Method not allowed.",
		"internal_error":         "Internal server error.",
	}
	message, ok := messages[code]
	if !ok {
		code, message = "internal_error", messages["internal_error"]
	}
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func serviceError(w http.ResponseWriter, err error) {
	var business *service.Error
	if !errors.As(err, &business) {
		fail(w, http.StatusInternalServerError, "internal_error")
		return
	}
	status, ok := map[string]int{
		"invalid_input": http.StatusBadRequest,
		"not_found":     http.StatusNotFound,
		"conflict":      http.StatusConflict,
		"unavailable":   http.StatusServiceUnavailable,
		"forbidden":     http.StatusForbidden,
	}[business.Code]
	if !ok {
		fail(w, http.StatusInternalServerError, "internal_error")
		return
	}
	fail(w, status, business.Code)
}

func object(w http.ResponseWriter, status int, value any, err error) {
	if err != nil {
		serviceError(w, err)
		return
	}
	writeJSON(w, status, map[string]any{"data": value})
}

func noContent(w http.ResponseWriter, err error) {
	if err != nil {
		serviceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeJSON strictly accepts one JSON object. Null is permitted only for the
// listed optional fields; everything else must be present as a non-null value.
func decodeJSON(w http.ResponseWriter, r *http.Request, target any, nullable ...string) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		fail(w, http.StatusUnsupportedMediaType, "unsupported_media_type")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(w, http.StatusRequestEntityTooLarge, "payload_too_large")
		} else {
			fail(w, http.StatusBadRequest, "invalid_input")
		}
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		fail(w, http.StatusBadRequest, "invalid_input")
		return false
	}
	allowedNull := map[string]bool{}
	for _, name := range nullable {
		allowedNull[name] = true
	}
	for key, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) && !allowedNull[key] {
			fail(w, http.StatusBadRequest, "invalid_input")
			return false
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		fail(w, http.StatusBadRequest, "invalid_input")
		return false
	}
	if decoder.Decode(new(any)) != io.EOF {
		fail(w, http.StatusBadRequest, "invalid_input")
		return false
	}
	return true
}

func pathID(r *http.Request) (int64, bool) {
	return decimal(r.PathValue("id"), false)
}

func decimal(text string, allowZero bool) (int64, bool) {
	if text == "" || strings.IndexFunc(text, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return 0, false
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil || value < 0 || (value == 0 && !allowZero) {
		return 0, false
	}
	return value, true
}

func require[T any](w http.ResponseWriter, value *T) (T, bool) {
	var zero T
	if value == nil {
		fail(w, http.StatusBadRequest, "invalid_input")
		return zero, false
	}
	return *value, true
}

func parseStrictQuery(raw string) (url.Values, bool) {
	values := url.Values{}
	if raw == "" {
		return values, true
	}
	for _, part := range strings.Split(raw, "&") {
		key, encoded, found := strings.Cut(part, "=")
		if !found || key == "" {
			return nil, false
		}
		name, err := url.QueryUnescape(key)
		if err != nil || name == "" {
			return nil, false
		}
		if _, duplicate := values[name]; duplicate {
			return nil, false
		}
		value, err := url.QueryUnescape(encoded)
		if err != nil {
			return nil, false
		}
		values.Set(name, value)
	}
	return values, true
}

func queryOnly(w http.ResponseWriter, r *http.Request, allowed ...string) (url.Values, bool) {
	values, ok := parseStrictQuery(r.URL.RawQuery)
	if !ok {
		fail(w, http.StatusBadRequest, "invalid_input")
		return nil, false
	}
	for key := range values {
		known := false
		for _, name := range allowed {
			if key == name {
				known = true
				break
			}
		}
		if !known {
			fail(w, http.StatusBadRequest, "invalid_input")
			return nil, false
		}
	}
	return values, true
}

func pagination(w http.ResponseWriter, r *http.Request, withStatus bool) (limit int, after int64, status string, ok bool) {
	allowed := []string{"limit", "after_id"}
	if withStatus {
		allowed = append(allowed, "status")
	}
	values, ok := queryOnly(w, r, allowed...)
	if !ok {
		return 0, 0, "", false
	}
	limit = 20
	after = 0
	if raw, present := values["limit"]; present {
		value, valid := decimal(raw[0], false)
		if !valid || value > 100 {
			fail(w, http.StatusBadRequest, "invalid_input")
			return 0, 0, "", false
		}
		limit = int(value)
	}
	if raw, present := values["after_id"]; present {
		value, valid := decimal(raw[0], true)
		if !valid {
			fail(w, http.StatusBadRequest, "invalid_input")
			return 0, 0, "", false
		}
		after = value
	}
	status = values.Get("status")
	if status == "" {
		status = "all"
	}
	switch status {
	case "all", "pending", "sending", "sent", "failed", "cancelled":
	default:
		fail(w, http.StatusBadRequest, "invalid_input")
		return 0, 0, "", false
	}
	return limit, after, status, true
}

func cursor(value *int64) *string {
	if value == nil {
		return nil
	}
	text := strconv.FormatInt(*value, 10)
	return &text
}

func servicePagination(w http.ResponseWriter, r *http.Request) (int, int64, service.ServiceFilter, bool) {
	filter := service.ServiceFilter{Status: "active"}
	values, ok := queryOnly(w, r, "q", "category_id", "status", "limit", "after_id")
	if !ok {
		return 0, 0, filter, false
	}
	invalid := func() (int, int64, service.ServiceFilter, bool) {
		fail(w, 400, "invalid_input")
		return 0, 0, filter, false
	}
	limit, after := 20, int64(0)
	if raw, present := values["limit"]; present {
		v, valid := decimal(raw[0], false)
		if !valid || v > 100 {
			return invalid()
		}
		limit = int(v)
	}
	if raw, present := values["after_id"]; present {
		v, valid := decimal(raw[0], true)
		if !valid {
			return invalid()
		}
		after = v
	}
	if raw, present := values["category_id"]; present {
		v, valid := decimal(raw[0], false)
		if !valid {
			return invalid()
		}
		filter.CategoryID = &v
	}
	if raw, present := values["status"]; present {
		if raw[0] == "" {
			return invalid()
		}
		filter.Status = raw[0]
	}
	switch filter.Status {
	case "active", "due_soon", "expired", "no_period", "archived", "all":
	default:
		return invalid()
	}
	filter.Query = strings.TrimSpace(values.Get("q"))
	if len([]rune(filter.Query)) > 200 {
		return invalid()
	}
	return limit, after, filter, true
}
