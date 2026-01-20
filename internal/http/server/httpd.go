// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package server // import "miniflux.app/v2/internal/http/server"

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"miniflux.app/v2/internal/api"
	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/fever"
	"miniflux.app/v2/internal/googlereader"
	"miniflux.app/v2/internal/http/request"
	"miniflux.app/v2/internal/storage"
	"miniflux.app/v2/internal/ui"
	"miniflux.app/v2/internal/version"
	"miniflux.app/v2/internal/worker"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// MARK: 启动 HTTP 服务器
func StartWebServer(store *storage.Storage, pool *worker.Pool) []*http.Server {
	listenAddresses := config.Opts.ListenAddr()
	var httpServers []*http.Server

	certFile := config.Opts.CertFile()
	keyFile := config.Opts.CertKeyFile()
	certDomain := config.Opts.CertDomain()
	var sharedAutocertTLSConfig *tls.Config

	if certDomain != "" {
		// 当配置了 certDomain 时，启用 ACME/autocert：在运行时自动签发/续期证书。
		// 这里会做两件事：
		// 1) 准备一份可复用的 tls.Config（后续用于真正的 HTTPS listener）
		// 2) 启动一个独立的 HTTP（80）服务，用于处理 ACME HTTP-01 challenge
		slog.Debug("Configuring autocert manager and shared TLS config", slog.String("domain", certDomain))
		certManager := autocert.Manager{
			// autocert 需要一个 Cache 用来持久化证书与 ACME 相关状态；这里用数据库表 acme_cache 实现。
			Cache: storage.NewCertificateCache(store),
			// 自动接受 CA 的服务条款（TOS），否则签发流程会被阻断。
			Prompt: autocert.AcceptTOS,
			// 限制只为指定域名签发证书，避免被当成“开放代理”滥用。
			HostPolicy: autocert.HostWhitelist(certDomain),
		}

		// 这份 tls.Config 会在后续 startAutoCertTLSServer() 中复用。
		// GetCertificate 会在握手阶段按 SNI 动态提供证书；证书不存在或临近过期时会触发 autocert 自动获取/续期。
		sharedAutocertTLSConfig = &tls.Config{}
		sharedAutocertTLSConfig.GetCertificate = certManager.GetCertificate
		// NextProtos 声明该服务支持的协议：
		// - "h2" / "http/1.1"：正常业务的 HTTP/2、HTTP/1.1
		// - acme.ALPNProto：允许 ACME 走 TLS-ALPN-01 challenge（某些环境下可能会用到）
		sharedAutocertTLSConfig.NextProtos = []string{"h2", "http/1.1", acme.ALPNProto}

		// ACME 的 HTTP-01 challenge 需要 CA 通过 80 端口回调校验。
		// 这里启动一个专门的 HTTP server，Handler 由 certManager.HTTPHandler(...) 包装，
		// 用于接管 `/.well-known/acme-challenge/` 路径并返回验证内容。
		challengeServer := &http.Server{
			Handler: certManager.HTTPHandler(nil),
			// ":http" 是 Go 的端口别名，等价于 ":80"。
			Addr: ":http",
		}
		slog.Info("Starting ACME HTTP challenge server for autocert", slog.String("address", challengeServer.Addr))
		go func() {
			// ListenAndServe 会阻塞运行；这里放到 goroutine 里，避免影响主启动流程。
			// 正常关闭时会返回 http.ErrServerClosed，这不算异常。
			if err := challengeServer.ListenAndServe(); err != http.ErrServerClosed {
				slog.Error("ACME HTTP challenge server failed", slog.Any("error", err))
			}
		}()
		// 启用 autocert 意味着会有 HTTPS listener；这里提前标记 HTTPS=true，供中间件/URL 生成等逻辑使用。
		config.Opts.SetHTTPSValue(true)
		// 将 challengeServer 纳入统一的 server 列表，便于后续统一关闭/回收资源。
		httpServers = append(httpServers, challengeServer)
	}

	for i, listenAddr := range listenAddresses {
		server := &http.Server{
			// 限制“读取整个请求（包含请求头+请求体）”允许花的最长时间
			ReadTimeout: config.Opts.HTTPServerTimeout(),
			// 限制 业务处理 + 写响应 允许花的最长时间
			// 超时后不会直接取消 handler 业务逻辑，但是写操作会失败/连接被断开，保护服务器资源
			WriteTimeout: config.Opts.HTTPServerTimeout(),
			// 限制 keep-alive 连接在“空闲状态”下能保持多久（也就是处理完一个请求后，等待下一个请求的最长空闲时间）
			// 控制长连接占用，释放不活跃连接，减少资源占用
			IdleTimeout: config.Opts.HTTPServerTimeout(),
			Handler:     setupHandler(store, pool),
		}

		if !strings.HasPrefix(listenAddr, "/") && os.Getenv("LISTEN_PID") != strconv.Itoa(os.Getpid()) {
			server.Addr = listenAddr
		}

		shouldAddServer := true

		switch {
		case os.Getenv("LISTEN_PID") == strconv.Itoa(os.Getpid()):
			if i == 0 {
				slog.Info("Starting server using systemd socket for the first listen address", slog.String("address_info", listenAddr))
				startSystemdSocketServer(server)
			} else {
				slog.Warn("Systemd socket activation: Only the first listen address is used by systemd. Other addresses ignored.", slog.String("skipped_address", listenAddr))
				shouldAddServer = false
			}
		case strings.HasPrefix(listenAddr, "/"): // Unix socket
			startUnixSocketServer(server, listenAddr)
		case certDomain != "" && (listenAddr == ":https" || (i == 0 && strings.Contains(listenAddr, ":"))):
			server.Addr = listenAddr
			startAutoCertTLSServer(server, sharedAutocertTLSConfig)
		case certFile != "" && keyFile != "":
			server.Addr = listenAddr
			startTLSServer(server, certFile, keyFile)
			config.Opts.SetHTTPSValue(true)
		default:
			server.Addr = listenAddr
			startHTTPServer(server)
		}

		if shouldAddServer {
			httpServers = append(httpServers, server)
		}
	}

	return httpServers
}

func startSystemdSocketServer(server *http.Server) {
	go func() {
		f := os.NewFile(3, "systemd socket")
		listener, err := net.FileListener(f)
		if err != nil {
			printErrorAndExit(`Unable to create listener from systemd socket: %v`, err)
		}

		slog.Info(`Starting server using systemd socket`)
		if err := server.Serve(listener); err != http.ErrServerClosed {
			printErrorAndExit(`Systemd socket server failed to start: %v`, err)
		}
	}()
}

func startUnixSocketServer(server *http.Server, socketFile string) {
	if err := os.Remove(socketFile); err != nil && !os.IsNotExist(err) {
		printErrorAndExit("Unable to remove existing Unix socket %s: %v", socketFile, err)
	}
	listener, err := net.Listen("unix", socketFile)
	if err != nil {
		printErrorAndExit(`Server failed to listen on Unix socket %s: %v`, socketFile, err)
	}

	if err := os.Chmod(socketFile, 0666); err != nil {
		printErrorAndExit(`Unable to change socket permission for %s: %v`, socketFile, err)
	}

	go func() {
		certFile := config.Opts.CertFile()
		keyFile := config.Opts.CertKeyFile()

		if certFile != "" && keyFile != "" {
			slog.Info("Starting TLS server using a Unix socket",
				slog.String("socket", socketFile),
				slog.String("cert_file", certFile),
				slog.String("key_file", keyFile),
			)
			// Ensure HTTPS is marked as true if any listener uses TLS
			config.Opts.SetHTTPSValue(true)
			if err := server.ServeTLS(listener, certFile, keyFile); err != http.ErrServerClosed {
				printErrorAndExit("TLS Unix socket server failed to start on %s: %v", socketFile, err)
			}
		} else {
			slog.Info("Starting server using a Unix socket", slog.String("socket", socketFile))
			if err := server.Serve(listener); err != http.ErrServerClosed {
				printErrorAndExit("Unix socket server failed to start on %s: %v", socketFile, err)
			}
		}
	}()
}

func startAutoCertTLSServer(server *http.Server, autoTLSConfig *tls.Config) {
	if server.TLSConfig == nil {
		server.TLSConfig = &tls.Config{}
	}
	server.TLSConfig.GetCertificate = autoTLSConfig.GetCertificate
	server.TLSConfig.NextProtos = autoTLSConfig.NextProtos

	go func() {
		slog.Info("Starting TLS server using automatic certificate management",
			slog.String("listen_address", server.Addr),
		)
		if err := server.ListenAndServeTLS("", ""); err != http.ErrServerClosed {
			printErrorAndExit("Autocert server failed to start on %s: %v", server.Addr, err)
		}
	}()
}

func startTLSServer(server *http.Server, certFile, keyFile string) {
	go func() {
		slog.Info("Starting TLS server using a certificate",
			slog.String("listen_address", server.Addr),
			slog.String("cert_file", certFile),
			slog.String("key_file", keyFile),
		)
		if err := server.ListenAndServeTLS(certFile, keyFile); err != http.ErrServerClosed {
			printErrorAndExit("TLS server failed to start on %s: %v", server.Addr, err)
		}
	}()
}

func startHTTPServer(server *http.Server) {
	go func() {
		slog.Info("Starting HTTP server",
			slog.String("listen_address", server.Addr),
		)
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			printErrorAndExit("HTTP server failed to start on %s: %v", server.Addr, err)
		}
	}()
}

func setupHandler(store *storage.Storage, pool *worker.Pool) *mux.Router {
	// livenessProbe：存活探针，用于告诉外部探测器“进程是否还活着”。
	// 一般只要进程能响应 HTTP 请求就返回 200，不依赖数据库等外部资源。
	livenessProbe := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}
	// readinessProbe：就绪探针，用于告诉外部探测器“服务是否已具备对外提供能力”。
	// 这里通过 store.Ping() 检查数据库连接是否可用，失败则返回 503。
	readinessProbe := func(w http.ResponseWriter, r *http.Request) {
		if err := store.Ping(); err != nil {
			http.Error(w, fmt.Sprintf("Database Connection Error: %q", err), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}

	// 根路由器：所有路由的入口。
	router := mux.NewRouter()

	// These routes do not take the base path into consideration and are always available at the root of the server.
	// 这组探针路由永远挂在根路径下（不受 BasePath 影响），方便容器编排/负载均衡做健康检查。
	router.HandleFunc("/liveness", livenessProbe).Name("liveness")
	router.HandleFunc("/healthz", livenessProbe).Name("healthz")
	router.HandleFunc("/readiness", readinessProbe).Name("readiness")
	router.HandleFunc("/readyz", readinessProbe).Name("readyz")

	var subrouter *mux.Router
	if config.Opts.BasePath() != "" {
		// 配置了 BasePath 时：业务路由都挂在该前缀下面（例如 /miniflux/...），探针路由仍在根路径。
		subrouter = router.PathPrefix(config.Opts.BasePath()).Subrouter()
	} else {
		// 未配置 BasePath 时：业务路由直接挂在根路径。
		subrouter = router.NewRoute().Subrouter()
	}

	if config.Opts.HasMaintenanceMode() {
		// 维护模式：对所有业务请求直接返回维护信息，不进入后续中间件与业务路由。
		subrouter.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(config.Opts.MaintenanceMessage()))
			})
		})
	}
	// MARK: 注册实际中间件和路由
	// 全局中间件：目前用于注入 client_ip、记录请求日志、按需设置 HSTS 等。
	subrouter.Use(middleware)

	// 注册各模块路由（Fever/Google Reader 兼容接口、API、UI）。
	fever.Serve(subrouter, store)
	googlereader.Serve(subrouter, store)
	if config.Opts.HasAPI() {
		api.Serve(subrouter, store, pool)
	}
	ui.Serve(subrouter, store, pool)

	// 兼容历史/外部集成使用的健康检查路径（受 BasePath 影响）。
	subrouter.HandleFunc("/healthcheck", readinessProbe).Name("healthcheck")

	// 版本信息接口（受 BasePath 影响）。
	subrouter.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(version.Version))
	}).Name("version")

	if config.Opts.HasMetricsCollector() {
		// 指标采集端点：使用 promhttp 提供的 handler 生成 /metrics 输出。
		subrouter.Handle("/metrics", promhttp.Handler()).Name("metrics")
		// 对 /metrics 做额外访问控制：未授权时返回 404（对外隐藏该端点的存在）。
		subrouter.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				route := mux.CurrentRoute(r)

				// Returns a 404 if the client is not authorized to access the metrics endpoint.
				if route.GetName() == "metrics" && !isAllowedToAccessMetricsEndpoint(r) {
					slog.Warn("Authentication failed while accessing the metrics endpoint",
						slog.String("client_ip", request.ClientIP(r)),
						slog.String("client_user_agent", r.UserAgent()),
						slog.String("client_remote_addr", r.RemoteAddr),
					)
					http.NotFound(w, r)
					return
				}

				next.ServeHTTP(w, r)
			})
		})
	}

	return router
}

func isAllowedToAccessMetricsEndpoint(r *http.Request) bool {
	clientIP := request.ClientIP(r)

	if config.Opts.MetricsUsername() != "" && config.Opts.MetricsPassword() != "" {
		username, password, authOK := r.BasicAuth()
		if !authOK {
			slog.Warn("Metrics endpoint accessed without authentication header",
				slog.Bool("authentication_failed", true),
				slog.String("client_ip", clientIP),
				slog.String("client_user_agent", r.UserAgent()),
				slog.String("client_remote_addr", r.RemoteAddr),
			)
			return false
		}

		if username == "" || password == "" {
			slog.Warn("Metrics endpoint accessed with empty username or password",
				slog.Bool("authentication_failed", true),
				slog.String("client_ip", clientIP),
				slog.String("client_user_agent", r.UserAgent()),
				slog.String("client_remote_addr", r.RemoteAddr),
			)
			return false
		}

		if username != config.Opts.MetricsUsername() || password != config.Opts.MetricsPassword() {
			slog.Warn("Metrics endpoint accessed with invalid username or password",
				slog.Bool("authentication_failed", true),
				slog.String("client_ip", clientIP),
				slog.String("client_user_agent", r.UserAgent()),
				slog.String("client_remote_addr", r.RemoteAddr),
			)
			return false
		}
	}

	remoteIP := request.FindRemoteIP(r)
	return request.IsTrustedIP(remoteIP, config.Opts.MetricsAllowedNetworks())
}

func printErrorAndExit(format string, a ...any) {
	message := fmt.Sprintf(format, a...)
	slog.Error(message)
	fmt.Fprintf(os.Stderr, "%v\n", message)
	os.Exit(1)
}
