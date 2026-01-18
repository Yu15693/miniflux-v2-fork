// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package metric // import "miniflux.app/v2/internal/metric"

import (
	"log/slog"
	"time"

	"miniflux.app/v2/internal/storage"

	"github.com/prometheus/client_golang/prometheus"
)

// Prometheus Metrics.
var (
	// 后台刷新订阅耗时（按成功/失败等状态打标签）
	BackgroundFeedRefreshDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "miniflux",
			Name:      "background_feed_refresh_duration",
			Help:      "Processing time to refresh feeds from the background workers",
			Buckets:   prometheus.LinearBuckets(1, 2, 15),
		},
		[]string{"status"},
	)

	// 内容抓取/解析耗时
	ScraperRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "miniflux",
			Name:      "scraper_request_duration",
			Help:      "Web scraper request duration",
			Buckets:   prometheus.LinearBuckets(1, 2, 25),
		},
		[]string{"status"},
	)

	// 归档条目耗时
	ArchiveEntriesDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "miniflux",
			Name:      "archive_entries_duration",
			Help:      "Archive entries duration",
			Buckets:   prometheus.LinearBuckets(1, 2, 30),
		},
		[]string{"status"},
	)

	// 用户总数
	usersGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "miniflux",
			Name:      "users",
			Help:      "Number of users",
		},
	)

	// 订阅总数（按状态分组）
	feedsGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "miniflux",
			Name:      "feeds",
			Help:      "Number of feeds by status",
		},
		[]string{"status"},
	)

	// 错误订阅数量
	brokenFeedsGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "miniflux",
			Name:      "broken_feeds",
			Help:      "Number of broken feeds",
		},
	)

	// 条目数量（按状态分组）
	entriesGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "miniflux",
			Name:      "entries",
			Help:      "Number of entries by status",
		},
		[]string{"status"},
	)

	// 数据库连接统计（连接池状态）
	dbOpenConnectionsGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "miniflux",
			Name:      "db_open_connections",
			Help:      "The number of established connections both in use and idle",
		},
	)

	dbConnectionsInUseGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "miniflux",
			Name:      "db_connections_in_use",
			Help:      "The number of connections currently in use",
		},
	)

	dbConnectionsIdleGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "miniflux",
			Name:      "db_connections_idle",
			Help:      "The number of idle connections",
		},
	)

	dbConnectionsWaitCountGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "miniflux",
			Name:      "db_connections_wait_count",
			Help:      "The total number of connections waited for",
		},
	)

	dbConnectionsMaxIdleClosedGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "miniflux",
			Name:      "db_connections_max_idle_closed",
			Help:      "The total number of connections closed due to SetMaxIdleConns",
		},
	)

	dbConnectionsMaxIdleTimeClosedGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "miniflux",
			Name:      "db_connections_max_idle_time_closed",
			Help:      "The total number of connections closed due to SetConnMaxIdleTime",
		},
	)

	dbConnectionsMaxLifetimeClosedGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "miniflux",
			Name:      "db_connections_max_lifetime_closed",
			Help:      "The total number of connections closed due to SetConnMaxLifetime",
		},
	)
)

// collector represents a metric collector.
type collector struct {
	store           *storage.Storage
	refreshInterval time.Duration
}

// NewCollector initializes a new metric collector.
func NewCollector(store *storage.Storage, refreshInterval time.Duration) *collector {
	// 注册指标到 Prometheus 默认注册表（必须注册后才能被采集）
	prometheus.MustRegister(BackgroundFeedRefreshDuration)
	prometheus.MustRegister(ScraperRequestDuration)
	prometheus.MustRegister(ArchiveEntriesDuration)
	prometheus.MustRegister(usersGauge)
	prometheus.MustRegister(feedsGauge)
	prometheus.MustRegister(brokenFeedsGauge)
	prometheus.MustRegister(entriesGauge)
	prometheus.MustRegister(dbOpenConnectionsGauge)
	prometheus.MustRegister(dbConnectionsInUseGauge)
	prometheus.MustRegister(dbConnectionsIdleGauge)
	prometheus.MustRegister(dbConnectionsWaitCountGauge)
	prometheus.MustRegister(dbConnectionsMaxIdleClosedGauge)
	prometheus.MustRegister(dbConnectionsMaxIdleTimeClosedGauge)
	prometheus.MustRegister(dbConnectionsMaxLifetimeClosedGauge)

	return &collector{store, refreshInterval}
}

// GatherStorageMetrics polls the database to fetch metrics.
func (c *collector) GatherStorageMetrics() {
	// 周期性拉取统计信息并写入 Gauge
	for range time.Tick(c.refreshInterval) {
		slog.Debug("Collecting metrics from the database")

		// 用户与订阅统计
		usersGauge.Set(float64(c.store.CountUsers()))
		brokenFeedsGauge.Set(float64(c.store.CountAllFeedsWithErrors()))

		feedsCount := c.store.CountAllFeeds()
		for status, count := range feedsCount {
			feedsGauge.WithLabelValues(status).Set(float64(count))
		}

		// 条目统计
		entriesCount := c.store.CountAllEntries()
		for status, count := range entriesCount {
			entriesGauge.WithLabelValues(status).Set(float64(count))
		}

		// 连接池统计
		dbStats := c.store.DBStats()
		dbOpenConnectionsGauge.Set(float64(dbStats.OpenConnections))
		dbConnectionsInUseGauge.Set(float64(dbStats.InUse))
		dbConnectionsIdleGauge.Set(float64(dbStats.Idle))
		dbConnectionsWaitCountGauge.Set(float64(dbStats.WaitCount))
		dbConnectionsMaxIdleClosedGauge.Set(float64(dbStats.MaxIdleClosed))
		dbConnectionsMaxIdleTimeClosedGauge.Set(float64(dbStats.MaxIdleTimeClosed))
		dbConnectionsMaxLifetimeClosedGauge.Set(float64(dbStats.MaxLifetimeClosed))
	}
}
