// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cli // import "miniflux.app/v2/internal/cli"

import (
	"fmt"

	"miniflux.app/v2/internal/storage"
)

func flushSessions(store *storage.Storage) {
	// 清空所有会话，让用户重新登录
	fmt.Println("Flushing all sessions (disconnect users)")
	if err := store.FlushAllSessions(); err != nil {
		printErrorAndExit(err)
	}
}
