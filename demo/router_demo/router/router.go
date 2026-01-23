package router

import (
	"context"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
)

type RouteName string

type CSRFMode uint8

const (
	CSRFIgnored CSRFMode = iota
	CSRFRequired
)

type RouteMeta struct {
	Public bool
	CSRF   CSRFMode
}

type RouteSpec struct {
	Name    RouteName
	Pattern string
	Methods []string
	Meta    RouteMeta
	Handler http.HandlerFunc
}

type routeEntry struct {
	route *mux.Route
	meta  RouteMeta
}

type ctxKey int

const (
	ctxKeyRouteName ctxKey = iota
	ctxKeyRouteMeta
	ctxKeyRouteParams
)

func RouteNameFromContext(r *http.Request) (RouteName, bool) {
	v := r.Context().Value(ctxKeyRouteName)
	name, ok := v.(RouteName)
	return name, ok
}

func RouteMetaFromContext(r *http.Request) (RouteMeta, bool) {
	v := r.Context().Value(ctxKeyRouteMeta)
	meta, ok := v.(RouteMeta)
	return meta, ok
}

func RouteParamsFromContext(r *http.Request) (map[string]string, bool) {
	v := r.Context().Value(ctxKeyRouteParams)
	params, ok := v.(map[string]string)
	return params, ok
}

type Router struct {
	muxRouter *mux.Router
	byName    map[RouteName]routeEntry
}

func New() *Router {
	m := mux.NewRouter()
	r := &Router{
		muxRouter: m,
		byName:    make(map[RouteName]routeEntry),
	}
	m.Use(r.injectRouteContext)
	return r
}

func (r *Router) Register(spec RouteSpec) error {
	if spec.Name == "" {
		return errors.New("empty route name")
	}
	if spec.Pattern == "" {
		return errors.New("empty route pattern")
	}
	if spec.Handler == nil {
		return errors.New("nil handler")
	}
	if _, exists := r.byName[spec.Name]; exists {
		return errors.New("duplicate route name: " + string(spec.Name))
	}

	route := r.muxRouter.HandleFunc(spec.Pattern, spec.Handler).Name(string(spec.Name))
	if len(spec.Methods) > 0 {
		route.Methods(spec.Methods...)
	}
	r.byName[spec.Name] = routeEntry{
		route: route,
		meta:  spec.Meta,
	}

	return nil
}

func (r *Router) URL(name RouteName, params map[string]string) (string, error) {
	entry, ok := r.byName[name]
	if !ok {
		return "", errors.New("route not found: " + string(name))
	}

	if len(params) > 0 {
		pairs := make([]string, 0, len(params)*2)
		for k, v := range params {
			pairs = append(pairs, k, v)
		}
		u, err := entry.route.URLPath(pairs...)
		if err != nil {
			return "", err
		}
		return u.Path, nil
	}
	u, err := entry.route.URLPath()
	if err != nil {
		return "", err
	}
	return u.Path, nil
}

func (r *Router) Use(mwf func(http.Handler) http.Handler) {
	r.muxRouter.Use(mux.MiddlewareFunc(mwf))
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.muxRouter.ServeHTTP(w, req)
}

func (r *Router) injectRouteContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		current := mux.CurrentRoute(req)
		if current == nil {
			next.ServeHTTP(w, req)
			return
		}

		name := RouteName(current.GetName())
		meta := r.byName[name].meta

		params := mux.Vars(req)
		paramsCopy := make(map[string]string, len(params))
		for k, v := range params {
			paramsCopy[k] = v
		}

		ctx := context.WithValue(req.Context(), ctxKeyRouteName, name)
		ctx = context.WithValue(ctx, ctxKeyRouteParams, paramsCopy)
		ctx = context.WithValue(ctx, ctxKeyRouteMeta, meta)
		next.ServeHTTP(w, req.WithContext(ctx))
	})
}
