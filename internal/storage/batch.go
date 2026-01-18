// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package storage // import "miniflux.app/v2/internal/storage"

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/urllib"
)

type BatchBuilder struct {
	db           *sql.DB
	args         []any
	conditions   []string
	batchSize    int
	limitPerHost int
}

func (s *Storage) NewBatchBuilder() *BatchBuilder {
	return &BatchBuilder{
		// 复用 Storage 的数据库连接池
		db: s.db,
	}
}

func (b *BatchBuilder) WithBatchSize(batchSize int) *BatchBuilder {
	b.batchSize = batchSize
	return b
}

func (b *BatchBuilder) WithUserID(userID int64) *BatchBuilder {
	// 追加条件与参数，占位符按参数个数递增
	b.conditions = append(b.conditions, "user_id = $"+strconv.Itoa(len(b.args)+1))
	b.args = append(b.args, userID)
	return b
}

func (b *BatchBuilder) WithCategoryID(categoryID int64) *BatchBuilder {
	// 追加条件与参数，占位符按参数个数递增
	b.conditions = append(b.conditions, "category_id = $"+strconv.Itoa(len(b.args)+1))
	b.args = append(b.args, categoryID)
	return b
}

func (b *BatchBuilder) WithErrorLimit(limit int) *BatchBuilder {
	// 只抓取解析错误次数未超过阈值的订阅
	if limit > 0 {
		b.conditions = append(b.conditions, "parsing_error_count < $"+strconv.Itoa(len(b.args)+1))
		b.args = append(b.args, limit)
	}
	return b
}

func (b *BatchBuilder) WithNextCheckExpired() *BatchBuilder {
	// 仅选择到期可刷新订阅
	b.conditions = append(b.conditions, "next_check_at < now()")
	return b
}

func (b *BatchBuilder) WithoutDisabledFeeds() *BatchBuilder {
	// 排除被禁用的订阅
	b.conditions = append(b.conditions, "disabled IS false")
	return b
}

func (b *BatchBuilder) WithLimitPerHost(limit int) *BatchBuilder {
	// 限制单个 hostname 在本批次的数量
	if limit > 0 {
		b.limitPerHost = limit
	}
	return b
}

// FetchJobs retrieves a batch of jobs based on the conditions set in the builder.
// When limitPerHost is set, it limits the number of jobs per feed hostname to prevent overwhelming a single host.
func (b *BatchBuilder) FetchJobs() (model.JobList, error) {
	// 基于条件构建 SQL，并按 next_check_at 取最早需要刷新的订阅
	query := `SELECT id, user_id, feed_url FROM feeds`

	if len(b.conditions) > 0 {
		// 将 builder 累积的条件拼成 WHERE 子句
		query += " WHERE " + strings.Join(b.conditions, " AND ")
	}

	// 先到期的订阅优先刷新
	query += " ORDER BY next_check_at ASC"

	if b.batchSize > 0 {
		// 限制批次大小，避免一次拉取过多任务
		query += " LIMIT " + strconv.Itoa(b.batchSize)
	}

	// 执行查询，参数按 WithXxx 的追加顺序绑定
	rows, err := b.db.Query(query, b.args...)
	if err != nil {
		return nil, fmt.Errorf(`store: unable to fetch batch of jobs: %v`, err)
	}
	defer rows.Close()

	// 预分配容量，减少 append 的扩容次数
	jobs := make(model.JobList, 0, b.batchSize)
	// 记录每个 hostname 已加入的数量，用于限流
	hosts := make(map[string]int)
	// 统计总行数与被限流跳过的数量
	nbRows := 0
	nbSkippedFeeds := 0

	for rows.Next() {
		var job model.Job
		if err := rows.Scan(&job.FeedID, &job.UserID, &job.FeedURL); err != nil {
			return nil, fmt.Errorf(`store: unable to fetch job record: %v`, err)
		}

		nbRows++

		if b.limitPerHost > 0 {
			// 避免同一站点在单批次中过度并发
			feedHostname := urllib.Domain(job.FeedURL)
			if hosts[feedHostname] >= b.limitPerHost {
				slog.Debug("Feed host limit reached for this batch",
					slog.String("feed_url", job.FeedURL),
					slog.String("feed_hostname", feedHostname),
					slog.Int("limit_per_host", b.limitPerHost),
					slog.Int("current", hosts[feedHostname]),
				)
				nbSkippedFeeds++
				continue
			}
			hosts[feedHostname]++
		}

		// 满足条件的任务加入批次
		jobs = append(jobs, job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(`store: error iterating on job records: %v`, err)
	}

	// 输出批次统计，便于观察限流效果
	slog.Info("Created a batch of feeds",
		slog.Int("batch_size", b.batchSize),
		slog.Int("rows_count", nbRows),
		slog.Int("skipped_feeds_count", nbSkippedFeeds),
		slog.Int("jobs_count", len(jobs)),
	)

	return jobs, nil
}
