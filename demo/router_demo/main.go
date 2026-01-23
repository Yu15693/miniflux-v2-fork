package main

import (
	"log"
	"net/http"

	"miniflux.app/v2/demo/router_demo/handlers"
	"miniflux.app/v2/demo/router_demo/middleware"
	"miniflux.app/v2/demo/router_demo/router"
)

func main() {
	r := router.New()
	h := handlers.Handlers{Router: r}

	mustRegister(r, router.RouteSpec{
		Name:    "healthz",
		Pattern: "/healthz",
		Methods: []string{http.MethodGet},
		Meta: router.RouteMeta{
			Public: true,
			CSRF:   router.CSRFIgnored,
		},
		Handler: h.Healthz,
	})

	mustRegister(r, router.RouteSpec{
		Name:    "login",
		Pattern: "/login",
		Methods: []string{http.MethodPost},
		Meta: router.RouteMeta{
			Public: true,
			CSRF:   router.CSRFIgnored,
		},
		Handler: h.Login,
	})

	mustRegister(r, router.RouteSpec{
		Name:    "settings",
		Pattern: "/settings",
		Methods: []string{http.MethodPost},
		Meta: router.RouteMeta{
			Public: false,
			CSRF:   router.CSRFRequired,
		},
		Handler: h.Settings,
	})

	mustRegister(r, router.RouteSpec{
		Name:    "feedEntry",
		Pattern: "/feed/{feedID}/entry/{entryID}",
		Methods: []string{http.MethodGet},
		Meta: router.RouteMeta{
			Public: false,
			CSRF:   router.CSRFIgnored,
		},
		Handler: h.FeedEntry,
	})

	chain := middleware.Default()
	r.Use(chain.Middleware())
	handler := r

	srv := &http.Server{
		Addr:    "127.0.0.1:8089",
		Handler: handler,
	}

	log.Printf("demo server listening on http://%s\n", srv.Addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func mustRegister(r *router.Router, spec router.RouteSpec) {
	if err := r.Register(spec); err != nil {
		panic(err)
	}
}
