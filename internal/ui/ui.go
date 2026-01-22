// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package ui // import "miniflux.app/v2/internal/ui"

import (
	"net/http"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/storage"
	"miniflux.app/v2/internal/template"
	"miniflux.app/v2/internal/worker"

	"github.com/gorilla/mux"
)

// Serve 会把「Web UI」相关的所有 HTTP 路由注册到给定的 router 上。
//
// 这里的 router 是整个应用的顶层路由器（通常还会承载 API、metrics 等其他模块），
// ui.go 通过创建一个子路由器（uiRouter）来集中管理 UI 的：
// - 会话（用户会话 / 应用会话）中间件
// - 模板引擎初始化与模板解析
// - 具体页面/动作对应的 handler 方法与路由规则
//
// 传入的依赖：
// - store：持久层访问入口，handler 会用它读取/写入数据
// - pool：后台任务执行池，handler 在需要异步工作（比如刷新订阅源）时会使用它
func Serve(router *mux.Router, store *storage.Storage, pool *worker.Pool) {
	// middleware 负责 UI 请求链路上的通用逻辑（比如校验/加载 Session、鉴权等）。
	// 这里把顶层 router 传进去，通常用于在中间件里做 URL 生成或反向查找路由等。
	middleware := newMiddleware(router, store)

	// 初始化模板引擎并解析所有模板文件。
	// 解析在启动阶段一次性完成，后续请求中直接渲染，避免每次请求都 IO/解析。
	templateEngine := template.NewEngine(router)
	templateEngine.ParseTemplates()

	// handler 是一组 HTTP handler 方法的集合，封装 UI 层的具体业务流程。
	// 每个路由绑定到 handler 的某个方法（例如 showUnreadPage / updateSettings 等）。
	handler := &handler{router, store, templateEngine, pool}

	// 创建 UI 专用子路由器：
	// - 与顶层路由隔离，便于统一添加中间件与规则
	// - 只影响 UI 路由，不影响其他模块路由
	uiRouter := router.NewRoute().Subrouter()

	// UI 请求的会话处理中间件：
	// - handleUserSession：通常负责加载当前用户/认证信息（例如基于 cookie/session）
	// - handleAppSession：通常负责加载应用级会话信息（例如用于 CSRF、偏好设置等）
	uiRouter.Use(middleware.handleUserSession)
	uiRouter.Use(middleware.handleAppSession)

	// StrictSlash(true) 会使路由对尾随斜杠更宽容：/foo 与 /foo/ 会被统一处理/重定向。
	// 这样可以减少因 URL 末尾斜杠差异导致的 404。
	uiRouter.StrictSlash(true)

	// Static assets.
	// 静态资源路由包含校验和（checksum）以实现长缓存（cache busting）：
	// - 文件内容变化 → checksum 变化 → URL 变化 → 浏览器重新拉取
	uiRouter.HandleFunc("/stylesheets/{name}.{checksum}.css", handler.showStylesheet).Name("stylesheet").Methods(http.MethodGet)
	uiRouter.HandleFunc("/{name}.{checksum}.js", handler.showJavascript).Name("javascript").Methods(http.MethodGet)
	uiRouter.HandleFunc("/favicon.ico", handler.showFavicon).Name("favicon").Methods(http.MethodGet)
	uiRouter.HandleFunc("/icon/{filename}", handler.showAppIcon).Name("appIcon").Methods(http.MethodGet)
	uiRouter.HandleFunc("/manifest.json", handler.showWebManifest).Name("webManifest").Methods(http.MethodGet)

	// New subscription pages.
	// 订阅新增流程（通常是添加 RSS/Atom 订阅源）：
	// - GET /subscribe：展示添加订阅页面
	// - POST /subscribe：提交订阅地址并创建订阅
	// - POST /subscriptions：提交选择结果（例如发现多个可订阅的 Feed 时）
	uiRouter.HandleFunc("/subscribe", handler.showAddSubscriptionPage).Name("addSubscription").Methods(http.MethodGet)
	uiRouter.HandleFunc("/subscribe", handler.submitSubscription).Name("submitSubscription").Methods(http.MethodPost)
	uiRouter.HandleFunc("/subscriptions", handler.showChooseSubscriptionPage).Name("chooseSubscription").Methods(http.MethodPost)
	uiRouter.HandleFunc("/bookmarklet", handler.bookmarklet).Name("bookmarklet").Methods(http.MethodGet)

	// Unread page.
	// 未读相关页面与动作：
	// - mark-all-as-read：全局标记为已读
	// - /unread：未读列表
	// - /unread/entry/{entryID}：未读条目详情页
	uiRouter.HandleFunc("/mark-all-as-read", handler.markAllAsRead).Name("markAllAsRead").Methods(http.MethodPost)
	uiRouter.HandleFunc("/unread", handler.showUnreadPage).Name("unread").Methods(http.MethodGet)
	uiRouter.HandleFunc("/unread/entry/{entryID}", handler.showUnreadEntryPage).Name("unreadEntry").Methods(http.MethodGet)

	// History pages.
	// 历史（已读）相关：
	// - /history：已读列表
	// - /history/entry/{entryID}：已读条目详情页
	// - /history/flush：清空已读历史（通常是批量删除或重置状态）
	uiRouter.HandleFunc("/history", handler.showHistoryPage).Name("history").Methods(http.MethodGet)
	uiRouter.HandleFunc("/history/entry/{entryID}", handler.showReadEntryPage).Name("readEntry").Methods(http.MethodGet)
	uiRouter.HandleFunc("/history/flush", handler.flushHistory).Name("flushHistory").Methods(http.MethodPost)

	// Starred pages.
	// 星标相关：
	// - /starred：星标列表
	// - /starred/entry/{entryID}：星标条目详情页
	uiRouter.HandleFunc("/starred", handler.showStarredPage).Name("starred").Methods(http.MethodGet)
	uiRouter.HandleFunc("/starred/entry/{entryID}", handler.showStarredEntryPage).Name("starredEntry").Methods(http.MethodGet)

	// Search pages.
	// 搜索相关：
	// - /search：搜索结果列表
	// - /search/entry/{entryID}：搜索结果条目详情页
	uiRouter.HandleFunc("/search", handler.showSearchPage).Name("search").Methods(http.MethodGet)
	uiRouter.HandleFunc("/search/entry/{entryID}", handler.showSearchEntryPage).Name("searchEntry").Methods(http.MethodGet)

	// Feed listing pages.
	// Feed 列表与批量刷新：
	// - /feeds：订阅源列表
	// - /feeds/refresh：触发所有 Feed 刷新（通常会投递后台任务到 worker pool）
	uiRouter.HandleFunc("/feeds", handler.showFeedsPage).Name("feeds").Methods(http.MethodGet)
	uiRouter.HandleFunc("/feeds/refresh", handler.refreshAllFeeds).Name("refreshAllFeeds").Methods(http.MethodGet)

	// Individual feed pages.
	// 单个 Feed 相关：
	// - refresh：刷新某个 Feed（GET/POST 都支持；另提供 forceRefresh 查询参数）
	// - edit/update/remove：编辑/更新/删除 Feed
	// - entries：Feed 下的条目列表/详情
	// - feedIcon：订阅源图标
	// - mark-all-as-read：仅标记该 Feed 为已读
	uiRouter.HandleFunc("/feed/{feedID}/refresh", handler.refreshFeed).Name("refreshFeed").Methods(http.MethodGet, http.MethodPost)
	uiRouter.HandleFunc("/feed/{feedID}/refresh", handler.refreshFeed).Queries("forceRefresh", "{forceRefresh:true|false}").Name("refreshFeed").Methods(http.MethodGet, http.MethodPost)
	uiRouter.HandleFunc("/feed/{feedID}/edit", handler.showEditFeedPage).Name("editFeed").Methods(http.MethodGet)
	uiRouter.HandleFunc("/feed/{feedID}/remove", handler.removeFeed).Name("removeFeed").Methods(http.MethodPost)
	uiRouter.HandleFunc("/feed/{feedID}/update", handler.updateFeed).Name("updateFeed").Methods(http.MethodPost)
	uiRouter.HandleFunc("/feed/{feedID}/entries", handler.showFeedEntriesPage).Name("feedEntries").Methods(http.MethodGet)
	uiRouter.HandleFunc("/feed/{feedID}/entries/all", handler.showFeedEntriesAllPage).Name("feedEntriesAll").Methods(http.MethodGet)
	uiRouter.HandleFunc("/feed/{feedID}/entry/{entryID}", handler.showFeedEntryPage).Name("feedEntry").Methods(http.MethodGet)
	uiRouter.HandleFunc("/unread/feed/{feedID}/entry/{entryID}", handler.showUnreadFeedEntryPage).Name("unreadFeedEntry").Methods(http.MethodGet)
	uiRouter.HandleFunc("/feed/icon/{externalIconID}", handler.showFeedIcon).Name("feedIcon").Methods(http.MethodGet)
	uiRouter.HandleFunc("/feed/{feedID}/mark-all-as-read", handler.markFeedAsRead).Name("markFeedAsRead").Methods(http.MethodPost)

	// Category pages.
	// 分类（Category）相关：
	// - 分类列表、创建、编辑、删除
	// - 分类与 Feed 关联管理
	// - 分类下条目列表（含 all/refresh/starred 等视图）
	// - 分类维度的 mark-all-as-read
	uiRouter.HandleFunc("/category/{categoryID}/entry/{entryID}", handler.showCategoryEntryPage).Name("categoryEntry").Methods(http.MethodGet)
	uiRouter.HandleFunc("/unread/category/{categoryID}/entry/{entryID}", handler.showUnreadCategoryEntryPage).Name("unreadCategoryEntry").Methods(http.MethodGet)
	uiRouter.HandleFunc("/starred/category/{categoryID}/entry/{entryID}", handler.showStarredCategoryEntryPage).Name("starredCategoryEntry").Methods(http.MethodGet)
	uiRouter.HandleFunc("/categories", handler.showCategoryListPage).Name("categories").Methods(http.MethodGet)
	uiRouter.HandleFunc("/category/create", handler.showCreateCategoryPage).Name("createCategory").Methods(http.MethodGet)
	uiRouter.HandleFunc("/category/save", handler.saveCategory).Name("saveCategory").Methods(http.MethodPost)
	uiRouter.HandleFunc("/category/{categoryID}/feeds", handler.showCategoryFeedsPage).Name("categoryFeeds").Methods(http.MethodGet)
	uiRouter.HandleFunc("/category/{categoryID}/feed/{feedID}/remove", handler.removeCategoryFeed).Name("removeCategoryFeed").Methods(http.MethodPost)
	uiRouter.HandleFunc("/category/{categoryID}/feeds/refresh", handler.refreshCategoryFeedsPage).Name("refreshCategoryFeedsPage").Methods(http.MethodGet)
	uiRouter.HandleFunc("/category/{categoryID}/entries", handler.showCategoryEntriesPage).Name("categoryEntries").Methods(http.MethodGet)
	uiRouter.HandleFunc("/category/{categoryID}/entries/refresh", handler.refreshCategoryEntriesPage).Name("refreshCategoryEntriesPage").Methods(http.MethodGet)
	uiRouter.HandleFunc("/category/{categoryID}/entries/all", handler.showCategoryEntriesAllPage).Name("categoryEntriesAll").Methods(http.MethodGet)
	uiRouter.HandleFunc("/category/{categoryID}/entries/starred", handler.showCategoryEntriesStarredPage).Name("categoryEntriesStarred").Methods(http.MethodGet)
	uiRouter.HandleFunc("/category/{categoryID}/edit", handler.showEditCategoryPage).Name("editCategory").Methods(http.MethodGet)
	uiRouter.HandleFunc("/category/{categoryID}/update", handler.updateCategory).Name("updateCategory").Methods(http.MethodPost)
	uiRouter.HandleFunc("/category/{categoryID}/remove", handler.removeCategory).Name("removeCategory").Methods(http.MethodPost)
	uiRouter.HandleFunc("/category/{categoryID}/mark-all-as-read", handler.markCategoryAsRead).Name("markCategoryAsRead").Methods(http.MethodPost)

	// Tag pages.
	// 标签（Tag）相关：通过 tagName 过滤条目列表/条目详情。
	uiRouter.HandleFunc("/tags/{tagName}/entries/all", handler.showTagEntriesAllPage).Name("tagEntriesAll").Methods(http.MethodGet)
	uiRouter.HandleFunc("/tags/{tagName}/entry/{entryID}", handler.showTagEntryPage).Name("tagEntry").Methods(http.MethodGet)

	// Entry pages.
	// 条目（Entry）动作类接口（更偏“操作”而非“页面”）：
	// - status：批量更新条目状态（已读/未读等）
	// - save：保存条目（例如收藏/稍后读之类，取决于业务定义）
	// - save-progression：保存媒体播放/阅读进度
	// - download：抓取全文/内容（通常会进行网络请求并写回存储）
	// - proxy：媒体代理（避免直连第三方资源、解决 mixed content/隐私等）
	// - star：切换星标
	uiRouter.HandleFunc("/entry/status", handler.updateEntriesStatus).Name("updateEntriesStatus").Methods(http.MethodPost)
	uiRouter.HandleFunc("/entry/save/{entryID}", handler.saveEntry).Name("saveEntry").Methods(http.MethodPost)
	uiRouter.HandleFunc("/entry/enclosure/{enclosureID}/save-progression", handler.saveEnclosureProgression).Name("saveEnclosureProgression").Methods(http.MethodPost)
	uiRouter.HandleFunc("/entry/download/{entryID}", handler.fetchContent).Name("fetchContent").Methods(http.MethodPost)
	uiRouter.HandleFunc("/proxy/{encodedDigest}/{encodedURL}", handler.mediaProxy).Name("proxy").Methods(http.MethodGet)
	uiRouter.HandleFunc("/entry/star/{entryID}", handler.toggleStarred).Name("toggleStarred").Methods(http.MethodPost)

	// Share pages.
	// 分享相关：
	// - share/unshare：创建/取消分享链接
	// - sharedEntry：通过 shareCode 访问公开分享页
	// - sharedEntries：分享列表
	uiRouter.HandleFunc("/entry/share/{entryID}", handler.createSharedEntry).Name("shareEntry").Methods(http.MethodPost)
	uiRouter.HandleFunc("/entry/unshare/{entryID}", handler.unshareEntry).Name("unshareEntry").Methods(http.MethodPost)
	uiRouter.HandleFunc("/share/{shareCode}", handler.sharedEntry).Name("sharedEntry").Methods(http.MethodGet)
	uiRouter.HandleFunc("/shares", handler.sharedEntries).Name("sharedEntries").Methods(http.MethodGet)

	// User pages.
	// 用户管理页面（通常只有管理员可用）：
	// - 列表、创建、编辑、删除
	uiRouter.HandleFunc("/users", handler.showUsersPage).Name("users").Methods(http.MethodGet)
	uiRouter.HandleFunc("/user/create", handler.showCreateUserPage).Name("createUser").Methods(http.MethodGet)
	uiRouter.HandleFunc("/user/save", handler.saveUser).Name("saveUser").Methods(http.MethodPost)
	uiRouter.HandleFunc("/users/{userID}/edit", handler.showEditUserPage).Name("editUser").Methods(http.MethodGet)
	uiRouter.HandleFunc("/users/{userID}/update", handler.updateUser).Name("updateUser").Methods(http.MethodPost)
	uiRouter.HandleFunc("/users/{userID}/remove", handler.removeUser).Name("removeUser").Methods(http.MethodPost)

	// Settings pages.
	// 设置与信息页：
	// - settings：用户偏好/系统设置
	// - integrations：第三方集成配置
	// - about：关于页面
	uiRouter.HandleFunc("/settings", handler.showSettingsPage).Name("settings").Methods(http.MethodGet)
	uiRouter.HandleFunc("/settings", handler.updateSettings).Name("updateSettings").Methods(http.MethodPost)
	uiRouter.HandleFunc("/integrations", handler.showIntegrationPage).Name("integrations").Methods(http.MethodGet)
	uiRouter.HandleFunc("/integration", handler.updateIntegration).Name("updateIntegration").Methods(http.MethodPost)
	uiRouter.HandleFunc("/about", handler.showAboutPage).Name("about").Methods(http.MethodGet)

	// Session pages.
	// 会话管理页面：
	// - sessions：列出当前用户的登录会话
	// - removeSession：注销某个会话
	uiRouter.HandleFunc("/sessions", handler.showSessionsPage).Name("sessions").Methods(http.MethodGet)
	uiRouter.HandleFunc("/sessions/{sessionID}/remove", handler.removeSession).Name("removeSession").Methods(http.MethodPost)

	// API Keys pages.
	// API Key 管理仅在启用 API 功能时暴露路由，避免在禁用 API 时泄漏入口或无意义页面。
	if config.Opts.HasAPI() {
		uiRouter.HandleFunc("/keys", handler.showAPIKeysPage).Name("apiKeys").Methods(http.MethodGet)
		uiRouter.HandleFunc("/keys/{keyID}/delete", handler.deleteAPIKey).Name("deleteAPIKey").Methods(http.MethodPost)
		uiRouter.HandleFunc("/keys/create", handler.showCreateAPIKeyPage).Name("createAPIKey").Methods(http.MethodGet)
		uiRouter.HandleFunc("/keys/save", handler.saveAPIKey).Name("saveAPIKey").Methods(http.MethodPost)
	}

	// OPML pages.
	// OPML 导入导出：
	// - export：导出订阅（GET 下载文件）
	// - import：展示导入页面
	// - upload/fetch：通过上传文件或远程抓取方式导入 OPML
	uiRouter.HandleFunc("/export", handler.exportFeeds).Name("export").Methods(http.MethodGet)
	uiRouter.HandleFunc("/import", handler.showImportPage).Name("import").Methods(http.MethodGet)
	uiRouter.HandleFunc("/upload", handler.uploadOPML).Name("uploadOPML").Methods(http.MethodPost)
	uiRouter.HandleFunc("/fetch", handler.fetchOPML).Name("fetchOPML").Methods(http.MethodPost)

	// OAuth2 flow.
	// OAuth2 相关路由仅在配置了 provider 时启用：
	// - unlink：解除绑定
	// - redirect/callback：授权跳转与回调
	if config.Opts.OAuth2Provider() != "" {
		uiRouter.HandleFunc("/oauth2/{provider}/unlink", handler.oauth2Unlink).Name("oauth2Unlink").Methods(http.MethodGet)
		uiRouter.HandleFunc("/oauth2/{provider}/redirect", handler.oauth2Redirect).Name("oauth2Redirect").Methods(http.MethodGet)
		uiRouter.HandleFunc("/oauth2/{provider}/callback", handler.oauth2Callback).Name("oauth2Callback").Methods(http.MethodGet)
	}

	// Offline page
	// 离线页：通常用于 PWA 或网络不可用时的兜底页面。
	uiRouter.HandleFunc("/offline", handler.showOfflinePage).Name("offline").Methods(http.MethodGet)

	// Authentication pages.
	// 认证相关：
	// - checkLogin：提交登录表单
	// - logout：退出登录
	// - login（GET /）：展示登录页，但会包一层 handleAuthProxy（例如支持反向代理认证或外部鉴权）
	uiRouter.HandleFunc("/login", handler.checkLogin).Name("checkLogin").Methods(http.MethodPost)
	uiRouter.HandleFunc("/logout", handler.logout).Name("logout").Methods(http.MethodGet)
	uiRouter.Handle("/", middleware.handleAuthProxy(http.HandlerFunc(handler.showLoginPage))).Name("login").Methods(http.MethodGet)

	// WebAuthn flow
	// WebAuthn 仅在启用后暴露路由：
	// - register/login 的 begin/finish 用于 WebAuthn 的挑战/响应两阶段流程
	// - delete/rename/save 用于凭证管理
	if config.Opts.WebAuthn() {
		uiRouter.HandleFunc("/webauthn/register/begin", handler.beginRegistration).Name("webauthnRegisterBegin").Methods(http.MethodGet)
		uiRouter.HandleFunc("/webauthn/register/finish", handler.finishRegistration).Name("webauthnRegisterFinish").Methods(http.MethodPost)
		uiRouter.HandleFunc("/webauthn/login/begin", handler.beginLogin).Name("webauthnLoginBegin").Methods(http.MethodGet)
		uiRouter.HandleFunc("/webauthn/login/finish", handler.finishLogin).Name("webauthnLoginFinish").Methods(http.MethodPost)
		uiRouter.HandleFunc("/webauthn/deleteall", handler.deleteAllCredentials).Name("webauthnDeleteAll").Methods(http.MethodPost)
		uiRouter.HandleFunc("/webauthn/{credentialHandle}/delete", handler.deleteCredential).Name("webauthnDelete").Methods(http.MethodPost)
		uiRouter.HandleFunc("/webauthn/{credentialHandle}/rename", handler.renameCredential).Name("webauthnRename").Methods(http.MethodGet)
		uiRouter.HandleFunc("/webauthn/{credentialHandle}/save", handler.saveCredential).Name("webauthnSave").Methods(http.MethodPost)
	}

	// robots.txt：默认禁止爬虫抓取整个站点（对于私有 RSS 阅读器更安全/更符合预期）。
	router.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("User-agent: *\nDisallow: /"))
	}).Name("robots")
}
