// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package database // import "miniflux.app/v2/internal/database"

import (
	"database/sql"
	"fmt"
	"log/slog"
)

// Migrate executes database migrations.
func Migrate(db *sql.DB) error {
	var currentVersion int
	// 读取当前 schema 版本（假设 schema_version 已存在）
	db.QueryRow(`SELECT version FROM schema_version`).Scan(&currentVersion)

	slog.Info("Running database migrations",
		slog.Int("current_version", currentVersion),
		slog.Int("latest_version", schemaVersion),
	)

	// 逐版本执行迁移，确保每次升级都在事务中完成
	for version := currentVersion; version < schemaVersion; version++ {
		newVersion := version + 1

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("[Migration v%d] %v", newVersion, err)
		}

		if err := migrations[version](tx); err != nil {
			tx.Rollback()
			return fmt.Errorf("[Migration v%d] %v", newVersion, err)
		}

		// 记录新 schema 版本（表内只保留一行）
		if _, err := tx.Exec(`TRUNCATE schema_version`); err != nil {
			tx.Rollback()
			return fmt.Errorf("[Migration v%d] %v", newVersion, err)
		}

		if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES ($1)`, newVersion); err != nil {
			tx.Rollback()
			return fmt.Errorf("[Migration v%d] %v", newVersion, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("[Migration v%d] %v", newVersion, err)
		}
	}

	return nil
}

// IsSchemaUpToDate checks if the database schema is up to date.
func IsSchemaUpToDate(db *sql.DB) error {
	var currentVersion int
	// 比对数据库版本与代码内最新版本
	db.QueryRow(`SELECT version FROM schema_version`).Scan(&currentVersion)
	if currentVersion < schemaVersion {
		return fmt.Errorf(`the database schema is not up to date: current=v%d expected=v%d`, currentVersion, schemaVersion)
	}
	return nil
}
