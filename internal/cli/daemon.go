// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cli // import "miniflux.app/v2/internal/cli"

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/http/server"
	"miniflux.app/v2/internal/metric"
	"miniflux.app/v2/internal/storage"
	"miniflux.app/v2/internal/systemd"
	"miniflux.app/v2/internal/worker"
)

func startDaemon(store *storage.Storage) {
	slog.Debug("Starting daemon...")

	// 监听退出信号，触发优雅关闭
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	signal.Notify(stop, syscall.SIGTERM)

	// 初始化后台任务池
	pool := worker.NewPool(store, config.Opts.WorkerPoolSize())

	// 启动定时任务（抓取/清理）
	if config.Opts.HasSchedulerService() && !config.Opts.HasMaintenanceMode() {
		runScheduler(store, pool)
	}

	var httpServers []*http.Server
	// 启动 HTTP 服务（UI/API/兼容接口）
	if config.Opts.HasHTTPService() {
		// StartWebServer 可能启动多个监听地址（对应 LISTEN_ADDR）
		httpServers = server.StartWebServer(store, pool)
	}

	// 启动指标采集
	if config.Opts.HasMetricsCollector() {
		// 指标采集在独立 goroutine 中周期性运行
		collector := metric.NewCollector(store, config.Opts.MetricsRefreshInterval())
		go collector.GatherStorageMetrics()
	}

	// systemd 就绪通知与 watchdog 机制
	if systemd.HasNotifySocket() {
		// 告诉 systemd：服务已就绪
		slog.Debug("Sending readiness notification to Systemd")

		if err := systemd.SdNotify(systemd.SdNotifyReady); err != nil {
			slog.Error("Unable to send readiness notification to systemd", slog.Any("error", err))
		}

		if config.Opts.HasWatchdog() && systemd.HasSystemdWatchdog() {
			slog.Debug("Activating Systemd watchdog")

			go func() {
				interval, err := systemd.WatchdogInterval()
				if err != nil {
					slog.Error("Unable to get watchdog interval from systemd", slog.Any("error", err))
					return
				}

				for {
					// watchdog 以 DB Ping 作为健康性判断
					if err := store.Ping(); err != nil {
						slog.Error("Unable to ping database", slog.Any("error", err))
					} else {
						// 定期发送 watchdog 心跳
						systemd.SdNotify(systemd.SdNotifyWatchdog)
					}

					time.Sleep(interval / 3)
				}
			}()
		}
	}

	<-stop
	slog.Debug("Shutting down the process")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 优雅关闭 HTTP 服务
	if len(httpServers) > 0 {
		slog.Debug("Shutting down HTTP servers...")
		for _, server := range httpServers {
			if server != nil {
				if err := server.Shutdown(ctx); err != nil {
					// 保留错误日志，但不中断其他 server 的关闭
					slog.Error("HTTP server shutdown error", slog.Any("error", err), slog.String("addr", server.Addr))
				}
			}
		}
		slog.Debug("All HTTP servers shut down.")
	} else {
		slog.Debug("No HTTP servers to shut down.")
	}

	slog.Debug("Process gracefully stopped")
}
