// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cli // import "miniflux.app/v2/internal/cli"

import (
	"log/slog"
	"time"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/metric"
	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/storage"
)

func runCleanupTasks(store *storage.Storage) {
	// 清理过期会话与归档旧文章
	nbSessions := store.CleanOldSessions(config.Opts.CleanupRemoveSessionsInterval())
	nbUserSessions := store.CleanOldUserSessions(config.Opts.CleanupRemoveSessionsInterval())
	slog.Info("Sessions cleanup completed",
		slog.Int64("application_sessions_removed", nbSessions),
		slog.Int64("user_sessions_removed", nbUserSessions),
	)

	// 归档已读文章，并记录耗时指标
	startTime := time.Now()
	if rowsAffected, err := store.ArchiveEntries(model.EntryStatusRead, config.Opts.CleanupArchiveReadInterval(), config.Opts.CleanupArchiveBatchSize()); err != nil {
		slog.Error("Unable to archive read entries", slog.Any("error", err))
	} else {
		slog.Info("Archiving read entries completed",
			slog.Int64("read_entries_archived", rowsAffected),
		)

		if config.Opts.HasMetricsCollector() {
			metric.ArchiveEntriesDuration.WithLabelValues(model.EntryStatusRead).Observe(time.Since(startTime).Seconds())
		}
	}

	// 归档未读文章（可用于长期归档）
	startTime = time.Now()
	if rowsAffected, err := store.ArchiveEntries(model.EntryStatusUnread, config.Opts.CleanupArchiveUnreadInterval(), config.Opts.CleanupArchiveBatchSize()); err != nil {
		slog.Error("Unable to archive unread entries", slog.Any("error", err))
	} else {
		slog.Info("Archiving unread entries completed",
			slog.Int64("unread_entries_archived", rowsAffected),
		)

		if config.Opts.HasMetricsCollector() {
			metric.ArchiveEntriesDuration.WithLabelValues(model.EntryStatusUnread).Observe(time.Since(startTime).Seconds())
		}
	}

	// 清理被删除条目的附件与正文内容
	if enclosuresAffected, err := store.DeleteRemovedEntriesEnclosures(); err != nil {
		slog.Error("Unable to delete enclosures from removed entries", slog.Any("error", err))
	} else {
		slog.Info("Deleting enclosures from removed entries completed",
			slog.Int64("removed_entries_enclosures_deleted", enclosuresAffected))
	}

	if contentAffected, err := store.ClearRemovedEntriesContent(config.Opts.CleanupArchiveBatchSize()); err != nil {
		slog.Error("Unable to clear content from removed entries", slog.Any("error", err))
	} else {
		slog.Info("Clearing content from removed entries completed",
			slog.Int64("removed_entries_content_cleared", contentAffected))
	}
}
