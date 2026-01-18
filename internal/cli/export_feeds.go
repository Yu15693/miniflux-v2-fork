// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cli // import "miniflux.app/v2/internal/cli"

import (
	"fmt"

	"miniflux.app/v2/internal/reader/opml"
	"miniflux.app/v2/internal/storage"
)

func exportUserFeeds(store *storage.Storage, username string) {
	// 查找用户并导出 OPML 订阅
	user, err := store.UserByUsername(username)
	if err != nil {
		printErrorAndExit(fmt.Errorf("unable to find user: %w", err))
	}

	if user == nil {
		printErrorAndExit(fmt.Errorf("user %q not found", username))
	}

	// 通过 OPML handler 输出订阅到 stdout
	opmlHandler := opml.NewHandler(store)
	opmlExport, err := opmlHandler.Export(user.ID)
	if err != nil {
		printErrorAndExit(fmt.Errorf("unable to export feeds: %w", err))
	}

	fmt.Println(opmlExport)
}
