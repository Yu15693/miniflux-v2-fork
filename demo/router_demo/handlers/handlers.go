package handlers

import (
	"encoding/json"
	"net/http"

	"miniflux.app/v2/demo/router_demo/router"
)

type Handlers struct {
	Router *router.Router
}

func (h Handlers) Healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK\n"))
}

func (h Handlers) Login(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:  "csrf",
		Value: "demo-token",
		Path:  "/",
	})

	settingsURL, err := h.Router.URL("settings", nil)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("unable to build url\n"))
		return
	}

	payload := map[string]any{
		"message":      "logged in (demo)",
		"settings_url": settingsURL,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(payload)
}

func (h Handlers) Settings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{
		"message": "settings (demo, auth required)",
	})
}

func (h Handlers) FeedEntry(w http.ResponseWriter, r *http.Request) {
	params, _ := router.RouteParamsFromContext(r)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{
		"feed_id":  params["feedID"],
		"entry_id": params["entryID"],
	})
}
