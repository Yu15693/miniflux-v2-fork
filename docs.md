# Miniflux v2 学习型导读

本文面向 Go 初学者（有 JS 全栈经验），帮助你快速理解 Miniflux v2 的功能、模块划分、目录结构，并给出一条可落地的学习路径与本地启动方式。

## 项目功能概览

- 极简 RSS/Atom/JSON Feed 阅读器
- 支持订阅管理、分类、收藏、搜索、分享
- 兼容 Google Reader / Fever API
- 支持多种第三方阅读/收藏/通知集成
- 强调隐私与安全（去追踪参数、媒体代理、内容净化）
- Go 单二进制部署，PostgreSQL 唯一依赖

## 模块划分（按职责）

1) 入口与 CLI
- 入口在 `main.go`，只做 CLI 解析并启动子命令
- CLI 实现在 `internal/cli/`，包含启动服务、初始化、健康检查等

2) 配置与基础设施
- `internal/config/` 解析环境变量与校验
- `internal/version/` 版本信息
- `internal/systemd/` systemd 通知

3) HTTP 服务与路由
- `internal/http/server/` HTTP 服务器与中间件
- `internal/http/route/` 路由注册
- `internal/http/request/` 和 `internal/http/response/` 请求/响应构建
- `internal/cookie/` Cookie 处理

4) Web UI 层
- `internal/ui/` 为网页端 handlers，按功能拆分
- `internal/template/` 模板渲染与函数
- `internal/ui/static/` 静态资源（通过 Go embed）

5) REST API 与兼容协议
- `internal/api/` REST API handlers
- `internal/googlereader/` Google Reader 兼容接口
- `internal/fever/` Fever API 兼容接口
- `client/` 为 Go API Client

6) 阅读器与内容处理管线（核心业务）
- `internal/reader/`：抓取、解析、净化、重写、可读性抽取
  - `fetcher/` HTTP 拉取与编码处理
  - `parser/` Atom/RSS/JSON 解析
  - `sanitizer/` 内容净化与安全处理
  - `rewrite/` URL/内容重写规则
  - `readability/` 全文抽取
  - `processor/` 额外处理（YouTube 等）

7) 数据访问与存储
- `internal/storage/` SQL 层，面向业务的存储接口
- `internal/database/` Postgres 连接与迁移
- `internal/model/` 核心业务模型

8) 用户与认证
- `internal/oauth2/` OAuth2/OIDC
- `internal/ui/form/` 表单校验与结构
- `internal/webauthn/` 相关数据结构在 `internal/model/` 和表单处理

9) 任务与后台作业
- `internal/worker/` 后台任务池
- `internal/cli/scheduler.go` 定时刷新逻辑

10) 国际化与工具
- `internal/locale/` 多语言翻译与格式化
- `internal/metric/` 指标
- `internal/mediaproxy/` 媒体代理

## 目录说明（顶层）

- `client/` Go 版 API 客户端
- `internal/` 服务端核心代码（大部分逻辑都在这里）
- `contrib/` 社区贡献内容（不保证可用）
- `packaging/` Docker、systemd、deb/rpm 等打包
- `.devcontainer/` 开发容器配置
- `main.go` 应用入口
- `Makefile` 构建、运行、测试
- `README.md` 项目功能与官方文档入口

## 建议学习路径（面向 Go + Web 服务）

1) 先跑起来并理解入口
- 读 `main.go` 与 `internal/cli/cli.go`，了解启动参数和命令
- 观察 `Makefile` 的 `run` 目标如何设置环境变量

2) 走一条“请求链路”
- 从 `internal/http/server/httpd.go` 看服务初始化
- 看 `internal/http/route/route.go` 的路由注册
- 选择一个 UI handler（如 `internal/ui/unread_entries.go`）追踪到 storage 层

3) 读数据访问层
- `internal/storage/` 看 SQL 构建和事务处理
- `internal/database/` 了解迁移与连接池
- `internal/model/` 对应数据结构

4) 读核心业务：订阅抓取与解析
- `internal/reader/fetcher/` 了解抓取流程
- `internal/reader/parser/` 了解不同 feed 解析
- `internal/reader/sanitizer/` 看内容清洗与安全策略

5) 了解 API 与兼容协议
- `internal/api/` 看 REST API 设计
- `internal/googlereader/` 与 `internal/fever/` 看兼容实现
- `client/` 看客户端调用方式

6) 进阶：集成、认证与后台任务
- `internal/integration/` 第三方服务对接
- `internal/oauth2/` 看 OAuth2/OIDC 流程
- `internal/worker/` 任务池与调度

## 本地启动（开发模式）

前置条件：Go >= 1.24、PostgreSQL。

1) 启动 Postgres（示例：Docker）

```bash
docker run --rm --name miniflux2-db -p 5432:5432 \
  -e POSTGRES_DB=miniflux2 \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  postgres
```

2) 设置数据库连接并运行

```bash
export DATABASE_URL='postgres://postgres:postgres@localhost/miniflux2?sslmode=disable'
make run
```

`make run` 会自动执行迁移并创建管理员账号（默认 admin / test123），并以 debug 模式启动。默认监听 `http://localhost:8080`。

如果你想手动运行：

```bash
RUN_MIGRATIONS=1 CREATE_ADMIN=1 ADMIN_USERNAME=admin ADMIN_PASSWORD=test123 \
  LOG_LEVEL=debug DATABASE_URL='postgres://postgres:postgres@localhost/miniflux2?sslmode=disable' \
  go run main.go
```

## 进一步资料

- `README.md`：功能概览与官方文档入口
- `miniflux.1`：命令行帮助（man page）
- 官方文档：https://miniflux.app/docs/

## 适合练手的改动点

- 给 UI 页面增加一个小的只读视图（不改业务流程）
- 在 `internal/reader/` 增加对某个站点的内容重写规则
- 为某个 storage 查询补充单元测试

