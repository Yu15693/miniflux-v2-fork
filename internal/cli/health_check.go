// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cli // import "miniflux.app/v2/internal/cli"

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"miniflux.app/v2/internal/config"
)

func doHealthCheck(healthCheckEndpoint string) {
	// auto 模式根据监听地址推导健康检查 URL
	if healthCheckEndpoint == "auto" {
		healthCheckEndpoint = "http://" + config.Opts.ListenAddr()[0] + config.Opts.BasePath() + "/healthcheck"
	}

	slog.Debug("Executing health check request", slog.String("endpoint", healthCheckEndpoint))

	// 发送 HTTP 请求并检查状态码
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(healthCheckEndpoint)
	if err != nil {
		printErrorAndExit(fmt.Errorf(`health check failure: %v`, err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		printErrorAndExit(fmt.Errorf(`health check failed with status code %d`, resp.StatusCode))
	}

	slog.Debug(`Health check is passing`)
}
