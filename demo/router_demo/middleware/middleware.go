package middleware

import (
	"net/http"

	"miniflux.app/v2/demo/router_demo/router"
)

type Chain struct {
	RequireAuth bool
	CSRFCookie  string
}

func Default() Chain {
	return Chain{
		RequireAuth: true,
		CSRFCookie:  "csrf",
	}
}

func (c Chain) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		handler := next
		handler = c.withCSRF(handler)
		handler = c.withAuth(handler)
		return handler
	}
}

func (c Chain) withAuth(next http.Handler) http.Handler {
	if !c.RequireAuth {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		meta, _ := router.RouteMetaFromContext(r)
		if meta.Public {
			next.ServeHTTP(w, r)
			return
		}

		user := r.Header.Get("X-Demo-User")
		if user == "" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("unauthorized\n"))
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (c Chain) withCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		meta, _ := router.RouteMetaFromContext(r)
		if meta.CSRF != router.CSRFRequired {
			next.ServeHTTP(w, r)
			return
		}

		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		csrfFromHeader := r.Header.Get("X-CSRF-Token")
		csrfFromCookie := ""
		if cookie, err := r.Cookie(c.CSRFCookie); err == nil {
			csrfFromCookie = cookie.Value
		}

		if csrfFromHeader == "" || csrfFromCookie == "" || csrfFromHeader != csrfFromCookie {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("invalid csrf\n"))
			return
		}

		next.ServeHTTP(w, r)
	})
}
