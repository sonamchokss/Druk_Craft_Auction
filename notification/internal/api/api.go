package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/sonamchokss/druk-craft-auction/notification/internal/store"
	"github.com/sonamchokss/druk-craft-auction/notification/internal/ws"
)

type API struct {
	Store *store.Store
	Hub   *ws.Hub
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", a.health)
	mux.HandleFunc("/notifications", a.notifications)
	mux.HandleFunc("/notifications/", a.read)
	mux.HandleFunc("/ws", a.userWS)
	mux.HandleFunc("/ws/events", a.eventWS)
	return cors(mux)
}
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Any origin, like the other services: the second demo laptop loads the
		// UI from http://<host-ip>:3000, which a fixed localhost origin blocks.
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,PATCH,OPTIONS")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
func (a *API) notifications(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("userId")
	if user == "" {
		http.Error(w, `{"message":"userId is required"}`, 400)
		return
	}
	items, err := a.Store.List(r.Context(), user, r.URL.Query().Get("unread") == "true")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	json.NewEncoder(w).Encode(items)
}
func (a *API) read(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PATCH" {
		http.NotFound(w, r)
		return
	}
	// PATCH /notifications/{id}/read (as documented); /notifications/{id} also works.
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/notifications/"), "/read")
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		http.Error(w, "invalid notification id", http.StatusBadRequest)
		return
	}
	if err := a.Store.MarkRead(r.Context(), id); err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *API) userWS(w http.ResponseWriter, r *http.Request) {
	a.Hub.ServeUser(w, r, r.URL.Query().Get("userId"))
}
func (a *API) eventWS(w http.ResponseWriter, r *http.Request) { a.Hub.ServeEvents(w, r) }
