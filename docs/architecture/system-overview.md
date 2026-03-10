# 系统架构总览

> 版本：v1.0
> 更新日期：2026-03-09
> 阶段：Phase 2 - 架构设计

---

## 1. 服务边界图

```
┌──────────────────────────────────────────────────────────────────────┐
│                            客户端层                                    │
│  ┌──────────────┐   ┌──────────────┐   ┌────────────────────────┐   │
│  │ Android App  │   │   iOS App    │   │       H5 页面           │   │
│  │  (Kotlin)    │   │   (Swift)    │   │   (Vanilla JS)          │   │
│  └──────┬───────┘   └──────┬───────┘   └───────────┬────────────┘   │
└─────────┼──────────────────┼───────────────────────┼────────────────┘
          │ HTTPS + WSS      │                        │
          └──────────────────┴────────────────────────┘
                             │
                     ┌───────▼────────┐
                     │  Nginx 反向代理  │
                     │  (TLS 终止)     │
                     └───────┬────────┘
                             │
          ┌──────────────────┴──────────────────────┐
          │                                          │
          │        ┌────────────────────┐            │
          │        │   Go API 服务       │            │
          │        │   (Gin HTTP)        │            │
          │        │                    │            │
          │        │ ┌────────────────┐ │            │
          │        │ │  REST API      │ │            │
          │        │ │  /api/v1/...   │ │            │
          │        │ └────────────────┘ │            │
          │        │ ┌────────────────┐ │            │
          │        │ │  WebSocket     │ │            │
          │        │ │  /ws           │ │            │
          │        │ └────────────────┘ │            │
          │        └────────┬───────────┘            │
          │                 │                        │
          │    ┌────────────┼───────────────┐        │
          │    │            │               │        │
          │  ┌─▼──┐   ┌─────▼──────┐  ┌────▼────┐   │
          │  │MySQL│   │   Redis    │  │七牛云OSS│   │
          │  │     │   │            │  │  CDN    │   │
          │  │用户  │   │Stream 队列  │  │音频文件  │   │
          │  │任务  │   │RefreshToken│  │         │   │
          │  │作品  │   │RateLimit   │  │         │   │
          │  │额度  │   │Pub/Sub     │  │         │   │
          │  └─────┘   └─────┬──────┘  └────▲────┘   │
          │                  │               │        │
          │        ┌─────────▼───────────┐   │        │
          │        │   Python 推理服务     │   │        │
          │        │ (FastAPI + Worker)   │───┘        │
          │        │                     │            │
          │        │ ┌─────────────────┐ │            │
          │        │ │  Stream Worker  │ │            │
          │        │ │  消费 Redis 任务  │ │            │
          │        │ └─────────────────┘ │            │
          │        │ ┌─────────────────┐ │            │
          │        │ │  AudioCraft     │ │            │
          │        │ │  MusicGen-Small │ │            │
          │        │ └─────────────────┘ │            │
          │        │ ┌─────────────────┐ │            │
          │        │ │  HTTP 回调       │ │            │
          │        │ │  → Go API       │ │            │
          │        │ └─────────────────┘ │            │
          │        └─────────────────────┘            │
          │                                           │
          │              AutoDL GPU 服务器              │
          └───────────────────────────────────────────┘
```

---

## 2. 核心数据流

### 2.1 音乐生成主流程

**操作顺序与失败补偿（POST /tasks）：**
1. 内容过滤（关键词黑名单）
2. 队列门禁：`GET calliope:queue:depth ≥ 20` → 返回 429 QUEUE_FULL（无状态变更）
3. **原子扣减额度**：`INSERT INTO credits ... ON DUPLICATE KEY UPDATE used = IF(used < limit_count, used+1, used)`；`ROW_COUNT()=0` → 返回 402 INSUFFICIENT_CREDITS
4. `INSERT tasks`（status=queued）；失败 → 退还额度（used-1），返回 500
5. **原子入队**：用 Lua 脚本将 `XADD` 和 `INCR calliope:queue:depth` 合并为一个原子操作（见下方说明）；失败 → 退还额度 + `UPDATE tasks SET status='failed'`，返回 500
6. 返回 202

> **步骤 5 原子性说明：** `XADD` 和 `INCR calliope:queue:depth` 单独执行时，若 `XADD` 成功而进程在 `INCR` 前崩溃，深度计数器将永久偏低。因此使用 Lua 脚本保证两个操作的原子性：
> ```lua
> local id = redis.call('XADD', KEYS[1], '*', unpack(ARGV))
> redis.call('INCR', KEYS[2])
> return id
> -- KEYS[1] = calliope:tasks:stream
> -- KEYS[2] = calliope:queue:depth
> -- ARGV    = task 消息字段（task_id, user_id, prompt, ...）
> ```
> 若 Redis 不支持 Lua（极少见），可在服务重启时通过 `SELECT COUNT(*) FROM tasks WHERE status IN ('queued','processing')` 重算计数器（见 database-schema.md §2.6）。

> **为何步骤 3 原子扣减能解决并发超扣：**
> `INSERT ON DUPLICATE KEY UPDATE used = IF(used < limit_count, used+1, used)` 在 MySQL 行锁保护下执行，`ROW_COUNT()` 返回 0 当且仅当 `used >= limit_count`，不存在"先读后写"的并发窗口。不需要也不应该在步骤 3 之前做"读校验"。

```
Client              Go API           Redis Stream      Python Worker       Qiniu OSS
  │                   │                   │                  │                 │
  │─ POST /tasks ────▶│                   │                  │                 │
  │                   │─ ①内容过滤         │                  │                 │
  │                   │─ ②depth门禁(≥20→429)▶│                 │                 │
  │                   │─ ③原子扣额度(失败→402)               │                 │
  │                   │─ ④INSERT tasks(queued)              │                 │
  │                   │─ ⑤Lua(XADD+INCR depth)▶│              │                 │
  │                   │  [失败→退额度+tasks=failed→500]        │                 │
  │◀─ 202 {task_id} ──│                   │                  │                 │
  │                   │                   │                  │                 │
  │─ WS /ws ─────────▶│                   │                  │                 │
  │◀─ {queued, pos:2} ─│                   │                  │                 │
  │                   │                   │─ XREADGROUP ─────▶│                 │
  │                   │                   │                  │─ HTTP 回调(processing)        │
  │                   │◀──────────────────────────────────── │  Bearer: INTERNAL_SECRET      │
  │                   │─ UPDATE tasks=processing             │                 │
  │                   │─ PUBLISH ws channel│                  │                 │
  │◀─ {processing,50%} ─                  │                  │                 │
  │                   │                   │                  │─ AudioCraft 推理  │
  │                   │                   │                  │  (30s ~ 3min)    │
  │                   │                   │                  │─ PUT audio ──────▶│
  │                   │                   │                  │─ HTTP 回调(completed,keys)    │
  │                   │◀──────────────────────────────────── │  Bearer: INTERNAL_SECRET      │
  │                   │─ UPDATE tasks=completed+keys         │                 │
  │                   │─ PUBLISH ws channel│                  │                 │
  │◀─ {completed} ────│                   │                  │                 │
  │                   │                   │                  │                 │
  │─ GET /tasks/:id ──▶│                   │                  │                 │
  │                   │─ 生成七牛云签名 URL   │                  │                 │
  │◀─ {candidates:[a_url, b_url]} ─────────│                  │                 │
  │                   │                   │                  │                 │
  │─ 直接请求 CDN URL ──────────────────────────────────────────────────────────▶│
  │◀─ 音频流（CDN 直传）─────────────────────────────────────────────────────────│
```

**所有 MySQL 写入（tasks 表状态变更）由 Go API 负责；Python Worker 只通过 HTTP 回调通知 Go API，不直接操作数据库。**

### 2.2 认证流程

```
Client                Go API              Redis
  │                      │                  │
  │─ POST /auth/register ▶│                  │
  │                      │─ bcrypt 密码      │
  │                      │─ INSERT users     │
  │                      │─ SET refresh:uid ▶│ TTL 7天
  │◀─ {access_token(15min), refresh_token} ──│
  │                      │                  │
  │─ API 请求 (Bearer AT) ▶│                  │
  │                      │─ 验证 JWT 签名     │
  │◀─ 正常响应 ─────────────│                  │
  │                      │                  │
  │  (15min 后 AT 过期)    │                  │
  │─ POST /auth/refresh ──▶│                  │
  │                      │─ GET refresh:uid ▶│
  │                      │◀─ token ──────────│
  │                      │─ 验证 token 匹配   │
  │                      │─ 颁发新 AT        │
  │◀─ {access_token} ─────│                  │
  │                      │                  │
  │─ POST /auth/logout ───▶│                  │
  │                      │─ DEL refresh:uid ▶│
  │◀─ 204 ────────────────│                  │
```

---

## 3. 部署架构（MVP 阶段）

```
Internet
    │
    │ HTTPS/WSS
    ▼
┌───────────────────────────────────────────────┐
│        阿里云 ECS (2C4G, Ubuntu 22.04)          │
│                                               │
│  ┌────────────────────────────────────────┐  │
│  │           Docker Compose               │  │
│  │                                        │  │
│  │  ┌──────────────┐  ┌────────────────┐  │  │
│  │  │    Nginx      │  │   Go API       │  │  │
│  │  │  :80 / :443   │──▶  container    │  │  │
│  │  │  (SSL 终止)    │  │  :8080        │  │  │
│  │  └──────────────┘  └───────┬────────┘  │  │
│  │                            │           │  │
│  │  ┌──────────────┐          │           │  │
│  │  │    Redis      │◀─────────┘           │  │
│  │  │  container    │                      │  │
│  │  │  :6379        │                      │  │
│  │  └──────────────┘                      │  │
│  └────────────────────────────────────────┘  │
│                                               │
│  ┌────────────────────────────────────────┐  │
│  │   阿里云 RDS MySQL（独立托管实例）         │  │
│  │   :3306（内网访问）                      │  │
│  └────────────────────────────────────────┘  │
└───────────────────────────────────────────────┘
          │ Redis Stream（公网 TLS + 密码 + 防火墙白名单 AutoDL IP）
          │ Go API 回调（HTTPS 公网，Bearer 共享密钥）
          ▼
┌───────────────────────────────────────────────┐
│       AutoDL GPU 服务器（按需启停）               │
│       RTX 3090 / 24GB VRAM                    │
│                                               │
│  Python 推理服务（Docker）                      │
│  FastAPI :8000（AutoDL 机器本地，不对外暴露）    │
│  Stream Worker + AudioCraft MusicGen-Small    │
└───────────────────────────────────────────────┘
          │ PUT audio files（HTTPS）
          ▼
┌───────────────────────────────────────────────┐
│                 七牛云                          │
│  ┌─────────────────┐  ┌─────────────────────┐ │
│  │  Kodo OSS        │  │       CDN           │ │
│  │  calliope-audio  │  │  音频分发加速          │ │
│  │  (私有空间)       │  │  时效签名 URL         │ │
│  └─────────────────┘  └─────────────────────┘ │
└───────────────────────────────────────────────┘
```

### 网络规划

| 组件 | 访问来源 | 访问方式 | 端口 |
|------|---------|---------|------|
| Nginx | 公网所有客户端 | 公网 HTTPS/WSS | 80, 443 |
| Go API | ECS 内部（Nginx 反代） | ECS 内网 | 8080 |
| Redis | ECS 内部 + AutoDL | ECS 内网（Go API）；**公网 TLS + requirepass + 防火墙仅放行 AutoDL IP 段**（Python Worker） | 6379 |
| MySQL (RDS) | ECS 内部 | ECS 内网 | 3306 |
| Go API（内部回调接口）| AutoDL Python Worker | 公网 HTTPS（`/internal/tasks/:id/status`，Bearer 共享密钥） | 443 |
| Python 推理服务 FastAPI | AutoDL 本机 | localhost | 8000 |
| 七牛云 CDN | 公网所有客户端 | 公网 HTTPS | 443 |

> **注意**：AutoDL 与阿里云 ECS 属于不同厂商，无法使用 VPC 内网互联，跨厂商通信走公网。安全措施：Redis 启用 TLS + 强密码 + 防火墙白名单；Go API 回调接口使用 Bearer 共享密钥鉴权。

---

## 4. Go API 服务目录结构

```
calliope-api/
├── cmd/
│   └── server/
│       └── main.go                  # 程序入口：加载配置、初始化依赖、启动 HTTP server
├── internal/
│   ├── config/
│   │   └── config.go                # 配置结构体（viper 加载），含 DB/Redis/Qiniu/JWT 配置
│   ├── handler/                     # HTTP 处理层（Gin handlers），只做参数绑定和响应格式化
│   │   ├── auth.go                  # POST /auth/register, /login, /refresh, /logout
│   │   ├── task.go                  # POST /tasks, GET /tasks/:id
│   │   ├── work.go                  # POST /works, GET /works, GET/PATCH/DELETE /works/:id
│   │   ├── credit.go                # GET /credits
│   │   └── websocket.go             # GET /ws（WebSocket 升级）
│   ├── middleware/
│   │   ├── auth.go                  # JWT 验证中间件，注入 user_id 到 context
│   │   ├── ratelimit.go             # IP 限流（Redis 固定窗口，30 req/min）
│   │   └── logger.go                # 结构化请求日志（JSON）
│   ├── service/                     # 业务逻辑层，不依赖 HTTP 框架
│   │   ├── auth_service.go          # 注册、登录、Token 颁发/刷新/撤销
│   │   ├── task_service.go          # 任务创建、状态查询、Python 回调处理、超时检测
│   │   ├── work_service.go          # 候选选择、作品列表、删除、下载 URL 生成
│   │   ├── credit_service.go        # 额度扣减（乐观锁）、退还、查询
│   │   └── notification_service.go  # WebSocket 连接管理、Redis Pub/Sub 订阅、消息推送
│   ├── repository/                  # 数据访问层，只做 SQL，不含业务逻辑
│   │   ├── user_repo.go
│   │   ├── task_repo.go
│   │   ├── work_repo.go
│   │   └── credit_repo.go
│   ├── queue/
│   │   └── redis_stream.go          # Redis Stream 生产者（XADD），封装消息格式
│   ├── storage/
│   │   └── qiniu.go                 # 七牛云 SDK 封装：生成私有空间签名下载 URL、批量删除
│   ├── model/                       # 数据库实体结构体（与表字段对应）
│   │   ├── user.go
│   │   ├── task.go
│   │   ├── work.go
│   │   └── credit.go
│   └── dto/                         # HTTP 请求/响应 DTO（与 OpenAPI schema 对应）
│       ├── auth_dto.go
│       ├── task_dto.go
│       └── work_dto.go
├── pkg/
│   ├── jwt/
│   │   └── jwt.go                   # JWT 签发（HS256）、验证、Claims 解析
│   ├── bcrypt/
│   │   └── hash.go                  # 密码哈希（cost=12）、验证
│   ├── filter/
│   │   └── content_filter.go        # 关键词黑名单过滤（prompt + lyrics）
│   └── response/
│       └── response.go              # 统一响应格式：{code, message, data}
├── migrations/                      # golang-migrate 迁移文件
│   ├── 000001_create_users.up.sql
│   ├── 000001_create_users.down.sql
│   ├── 000002_create_tasks.up.sql
│   ├── 000002_create_tasks.down.sql
│   ├── 000003_create_works.up.sql
│   ├── 000003_create_works.down.sql
│   ├── 000004_create_credits.up.sql
│   ├── 000004_create_credits.down.sql
│   ├── 000005_create_login_attempts.up.sql
│   └── 000005_create_login_attempts.down.sql
├── docker/
│   └── Dockerfile
├── go.mod
├── go.sum
└── .env.example
```

---

## 5. Python 推理服务目录结构

```
calliope-inference/
├── app/
│   ├── main.py                       # FastAPI 入口，注册路由，挂载 Worker 启动
│   ├── config.py                     # 配置加载（pydantic-settings），含 Redis/Qiniu/GoAPI 配置
│   ├── api/
│   │   └── internal.py               # 内部接口（Go API 可调用）
│   │                                 #   GET  /health
│   │                                 #   GET  /internal/queue/stats（队列长度、Worker 状态）
│   ├── worker/
│   │   ├── stream_consumer.py        # Redis Stream XREADGROUP 消费主循环，异常重试
│   │   └── task_handler.py           # 单任务处理编排：拿到消息 → 推理 → 上传 → 回调
│   ├── inference/
│   │   ├── base.py                   # InferenceBackend 抽象接口（generate 方法）
│   │   ├── musicgen.py               # AudioCraft MusicGen 实现（本地 GPU）
│   │   └── siliconflow.py            # SiliconFlow API 实现（阶段 0，无 GPU 替代）
│   ├── storage/
│   │   └── qiniu.py                  # 七牛云上传（PUT /audio/{user_id}/{task_id}/candidate_x.mp3）
│   ├── callback/
│   │   └── go_api.py                 # HTTP 回调 Go API（POST /internal/tasks/:id/status）
│   └── models/
│       └── task.py                   # Task Pydantic 模型（与 Redis Stream 消息字段对应）
├── tests/
│   ├── test_stream_consumer.py
│   ├── test_task_handler.py
│   └── test_inference_mock.py        # 用 Mock InferenceBackend 跑完整链路测试
├── docker/
│   └── Dockerfile.gpu                # CUDA 12.1 基础镜像 + PyTorch + AudioCraft
├── requirements.txt
├── requirements-dev.txt
└── .env.example
```

---

## 6. 关键设计决策摘要

| 决策点 | 选择 | 核心理由 |
|--------|------|---------|
| 服务数量 | 2 个（Go API + Python Worker） | 避免过度拆分，WebSocket 内嵌到 Go API，goroutine 天然适合高并发连接 |
| 任务队列 | Redis Stream | GPU 是吞吐瓶颈，无需 Kafka 级别容量；省掉一个重量级组件 |
| WebSocket 多实例扩展 | Redis Pub/Sub | Worker 回调任意 Go API 实例，通过 Pub/Sub 广播到持有连接的实例 |
| 认证方案 | JWT(15min) + Redis RefreshToken(7天) | Access Token 无状态减少 Redis 查询；Refresh Token 存 Redis 支持主动撤销 |
| 音频分发 | 七牛云 CDN + 时效签名 URL(1h) | 客户端直连 CDN，Go API 不做音频流中转，带宽成本归零 |
| Python Worker 回调方式 | HTTP 回调 Go API（Bearer 共享密钥） | 简单可靠；Worker 不直接操作 MySQL，业务逻辑内聚在 Go 层；共享密钥防止伪造回调 |
| 推理后端抽象 | InferenceBackend 接口 | 阶段 0 用 SiliconFlow API，后期换 AudioCraft 本地 GPU，零改动上层代码 |
| 额度并发安全 | MySQL 行锁（INSERT ON DUPLICATE KEY UPDATE used = IF(used < limit_count, used+1, used)，ROW_COUNT()=0 即额度满） | 写即检查，无需先读后写；MySQL 行锁保证原子性，不依赖 Redis |
