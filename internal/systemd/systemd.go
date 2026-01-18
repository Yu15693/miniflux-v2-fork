// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package systemd // import "miniflux.app/v2/internal/systemd"

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"
)

const (
	// SdNotifyReady tells the service manager that service startup is
	// finished, or the service finished loading its configuration.
	// https://www.freedesktop.org/software/systemd/man/sd_notify.html#READY=1
	SdNotifyReady = "READY=1"

	// SdNotifyWatchdog the service manager to update the watchdog timestamp.
	// https://www.freedesktop.org/software/systemd/man/sd_notify.html#WATCHDOG=1
	SdNotifyWatchdog = "WATCHDOG=1"
)

// HasNotifySocket checks if the process is supervised by Systemd and has the notify socket.
func HasNotifySocket() bool {
	// NOTIFY_SOCKET 由 systemd 注入，用于 sd_notify 通信
	return os.Getenv("NOTIFY_SOCKET") != ""
}

// HasSystemdWatchdog checks if the watchdog is configured in Systemd unit file.
func HasSystemdWatchdog() bool {
	// WATCHDOG_USEC 由 systemd 注入，表示 watchdog 超时间隔（微秒）
	return os.Getenv("WATCHDOG_USEC") != ""
}

// WatchdogInterval returns the watchdog interval configured in systemd unit file.
func WatchdogInterval() (time.Duration, error) {
	// 将环境变量的微秒值转换为 time.Duration
	s, err := strconv.Atoi(os.Getenv("WATCHDOG_USEC"))
	if err != nil {
		return 0, fmt.Errorf(`systemd: error converting WATCHDOG_USEC: %v`, err)
	}

	if s <= 0 {
		return 0, errors.New(`systemd: error WATCHDOG_USEC must be a positive number`)
	}

	return time.Duration(s) * time.Microsecond, nil
}

// SdNotify sends a message to systemd using the sd_notify protocol.
// See https://www.freedesktop.org/software/systemd/man/sd_notify.html.
func SdNotify(state string) error {
	// systemd 使用 Unix datagram socket 接收服务状态通知
	addr := &net.UnixAddr{
		Net:  "unixgram",
		Name: os.Getenv("NOTIFY_SOCKET"),
	}

	if addr.Name == "" {
		// We're not running under systemd (NOTIFY_SOCKET is not set).
		return nil
	}

	conn, err := net.DialUnix(addr.Net, nil, addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err = conn.Write([]byte(state)); err != nil {
		return err
	}

	return nil
}
