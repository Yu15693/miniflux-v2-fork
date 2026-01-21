// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package server // import "miniflux.app/v2/internal/http/server"

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/http/request"
)

func middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 获得并判断 Remote Client IP 是否为可信网络
		remoteIP := request.FindRemoteIP(r)
		isTrustedProxyClientIP := request.IsTrustedIP(remoteIP, config.Opts.TrustedReverseProxyNetworks())
		// 获得 Real Client IP
		clientIP := request.FindClientIP(r, isTrustedProxyClientIP)
		ctx := r.Context()
		ctx = context.WithValue(ctx, request.ClientIPContextKey, clientIP)

		if isTrustedProxyClientIP && r.Header.Get("X-Forwarded-Proto") == "https" {
			config.Opts.SetHTTPSValue(true)
		}

		t1 := time.Now()
		defer func() {
			slog.Debug("Incoming request",
				slog.String("client_ip", clientIP),
				slog.Group("request",
					// 用了 group 后类似 request.method=GET
					slog.String("method", r.Method),
					slog.String("uri", r.RequestURI),
					slog.String("protocol", r.Proto),
					slog.Duration("execution_time", time.Since(t1)),
				),
			)
		}()

		if config.Opts.HTTPS() && config.Opts.HasHSTS() {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}

		// r.WithContext 在中间件里替换 context 再传给下游
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
