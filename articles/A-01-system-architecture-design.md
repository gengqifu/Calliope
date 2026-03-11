# 从零设计 AI 音乐生成系统：架构选型与高并发方案

> 本文是 Calliope 项目系列文章的第一篇。Calliope 是一个模仿 Suno/Udio 的 AI 音乐生成系统，支持 Android、iOS、H5 三端。本文记录架构设计阶段的全部决策过程——不仅写"选了什么"，更写"为什么选"以及"踩过哪些坑"。

---

## 背景

作为一名 Android 架构师，我一直想深入系统后端与 AI 工程。于是决定做一个项目：从零搭建一套 AI 音乐生成系统，目标对标 Suno/Udio，支持三端（Android Kotlin / iOS Swift / H5）。

附加目标：**用这个项目从 Android 架构师成长为后端/系统/AI 架构师**，所以技术选型优先学习价值而非最短路径。

---

## 系统整体架构

先看最终的架构全貌，再逐一拆解每个决策。

### 两服务 vs 微服务

最初我考虑拆成三个服务：API 网关 + 业务服务 + WebSocket 服务。拆开的理由也很充分——职责分离，可以独立扩展。

但仔细分析后，我选择了**两服务架构**：

```
Go API 服务（业务层）
├── REST API（用户认证、任务管理、作品管理）
├── WebSocket（实时任务进度推送）
└── 内部回调接口（接收 Python Worker 的结果通知）

Python 推理服务（AI 层）
├── Redis Stream Worker（消费任务队列）
├── AudioCraft MusicGen（音乐生成模型）
└── 七牛云上传（把生成的音频写到 OSS）
```

**把 WebSocket 内嵌到 Go API 的理由：**
- Go 的 goroutine 模型天然适合大量长连接。每个 WebSocket 连接一个 goroutine，初始栈 ~2KB，调度器自动管理。MVP 目标 50 人同时在线，goroutine 模型轻松应对，完全不需要为此单独起一个进程。
- 拆开的成本：多了一跳网络调用、多了一个部署单元、多了跨服务的连接状态同步问题。

**两服务如何通信？** 不直接调用，通过 Redis Stream 解耦：

```
┌──────────────────────────────────────────────────────────┐
│                        客户端层                            │
│    Android App       iOS App         H5 页面              │
└──────────────────────┬───────────────────────────────────┘
                       │ HTTPS + WSS
                ┌──────▼──────┐
                │  Nginx 反代  │（TLS 终止）
                └──────┬──────┘
                       │
          ┌────────────▼────────────┐
          │       Go API 服务        │
          │  REST API + WebSocket   │
          └──┬──────────┬───────────┘
             │          │
          ┌──▼──┐   ┌───▼────────────┐
          │MySQL│   │    Redis        │
          │     │   │ Stream 队列      │◀── Python Worker 消费
          └─────┘   │ RefreshToken   │
                    │ Pub/Sub 通知    │
                    └────────────────┘
                            │
                ┌───────────▼──────────┐
                │   Python 推理服务      │
                │  AutoDL RTX 3090     │
                │  AudioCraft MusicGen │
                └───────────┬──────────┘
                            │ 上传音频
                    ┌───────▼───────┐
                    │   七牛云 OSS    │
                    │   + CDN 分发   │
                    └───────────────┘
```

---

## 核心数据流：一次音乐生成请求

用户点"生成"到听到音乐，经历了什么？

```
Client → POST /tasks
  ①  内容过滤（关键词黑名单）
  ②  队列门禁（depth ≥ 20 → 429 QUEUE_FULL，软上限 best-effort，见下方说明）
  ③  原子扣减额度（MySQL 行锁，失败 → 402 INSUFFICIENT_CREDITS）
  ④  INSERT tasks（status=queued）
  ⑤  Lua 脚本原子执行 XADD + INCR depth
      [失败 → 开启新补偿事务，顺序执行两步：
        1. CAS 更新 task：UPDATE tasks SET status='failed' WHERE id=? AND status='queued'，检查 affected rows=1（门闩）；若为 0 说明已被并发补偿，直接跳过步骤 2
        2. 仅当步骤 1 成功时退额度：UPDATE credits SET used=used-1 WHERE user_id=? AND date=? AND used>0
        额度退还以 task 状态转换成功为唯一前提，防止并发重试重复退减
        若步骤 2 失败（DB 抖动等），记录错误日志，由对账扫描兜底；tasks 表需增加 `credit_refunded TINYINT(1) DEFAULT 0` 字段：步骤 2 成功后置 1，对账扫描条件为 `status IN ('failed') AND credit_refunded=0`，退额度成功后再置 1；对账判定基于字段而非计数推算，幂等且可直接查询，不会重复退也不会漏退]
      [崩溃场景分两类：
        · ③+④ 提交前崩溃 → MySQL 整体回滚，无残留记录，不需处理
        · ③+④ 已提交，⑤ 执行前进程崩溃 → tasks 停留 queued 态，启动时对齐扫描（status=queued 且超龄 > 90min）负责清理并退还额度]
      [超龄阈值推导：GPU 冷启动 3min + 最大排队等待 20个×3min = 63min，取 90min（含安全余量）作为保守阈值；此阈值应随推理超时上限、GPU 并发数和最大队列深度联动调整，不应硬编码，运行时配置变更时需同步更新]
      [实现顺序约束：③+④ 必须放在同一 MySQL 事务内提交，再单独执行 ⑤ Lua XADD；禁止将三步包进同一 MySQL 事务在 Lua 之后提交——否则 Lua 成功但 MySQL commit 失败会产生"幽灵任务"（Stream 有消息但 DB 无记录，Worker 回调 404 后 XACK，任务消失）；③④分开提交同样有问题：若③提交后④失败，额度已扣但无任务记录]
  ⑥  返回 202 {task_id}

Client → WS /ws（订阅进度）
  ← {queued, position: 2}

Python Worker → XREADGROUP（取到任务）
  → HTTP 回调 Go API（status=processing）
  ← Go API 更新 MySQL + PUBLISH ws channel
  ← {processing, progress: 50%}

Python Worker → AudioCraft 推理（30s ~ 3min）
  → 上传两个候选音频到七牛云
  → HTTP 回调 Go API（status=completed, candidate_keys）
  ← Go API 更新 MySQL + PUBLISH ws channel
  ← {completed}

Client → GET /tasks/:id（获取候选音频 URL）
  ← {candidates: [signed_url_a, signed_url_b]}

Client → 直接请求七牛云 CDN URL
  ← 音频流（CDN 直传，Go API 不做音频中转）
```

这里有几个重要的设计决策，逐一展开。

> **幂等性说明：** 当前 POST /tasks 不支持幂等键，客户端超时重试会产生重复任务并重复扣额度。解决方案是客户端生成 UUID 作为 `X-Idempotency-Key`，服务端在 Redis 中以 `(user_id + idempotency_key)` 作为复合缓存键存储 `→ task_id`（TTL 24 小时），重复请求直接返回已有任务 ID。复合键确保不同用户的 key 互不干扰，也防止恶意用户通过猜测他人 key 获取他人任务信息。纯 Redis 缓存属于弱幂等：若 Redis 重启或 key 被淘汰，重放请求会绕过缓存再次创建任务并扣额度。强幂等需在 tasks 表增加 `UNIQUE KEY (user_id, idempotency_key)` 兜底——重放时 INSERT 触发唯一键冲突，应用层捕获后查询已有 task_id 返回；Redis 层仅作加速缓存，不承担唯一性保证。MVP 阶段内部用户不配置自动重试、手动重试前会看到 queued 状态，重复创建概率极低，可暂缓实现；正式开放前必须补全。正式实现时还应同时校验请求体哈希（如对 prompt+lyrics+mode 取 SHA256），key 相同但内容不同时返回 409，防止客户端 bug 复用 key 导致参数更新静默失效。重放请求（key 命中且内容匹配）返回 **202**（与首次创建一致）并附加响应头 `Idempotency-Replayed: true`，让客户端可区分是新建还是重放。唯一键 `UNIQUE KEY (user_id, idempotency_key)` 是 DB 级长期约束，与 Redis 的 24h TTL 语义不同：Redis TTL 到期仅意味着缓存失效，不代表允许重建——相同 key 重新 INSERT 仍会被唯一键拦截并返回已有 task_id，不会产生新任务。客户端如需创建新任务必须生成新的 UUID key。

> **步骤②软上限说明：** 门禁检查（`GET calliope:queue:depth`）和入队（Lua XADD+INCR）之间没有分布式锁，并发下两个请求可能同时读到 depth=19，都通过检查，最终 depth 变成 21。这是已知的并发穿透窗口，20 是 best-effort 软上限而非严格容量保证。对 MVP 内测场景（10 个用户，低并发）可接受；若需要严格上限，需将门禁检查合并进 Lua 脚本做原子判断。

---

## 决策一：API 层用 Go + Gin

### 为什么不用 Python FastAPI？

最直观的想法是：AI 推理层已经用 Python 了，API 层也用 Python，统一语言，维护方便。

但仔细想之后选择了 Go：

| 维度 | Go + Gin | Python + FastAPI |
|------|----------|-----------------|
| WebSocket 并发 | goroutine 天然适合，每连接 ~2KB | asyncio 可以，但 async/await 传染性强 |
| 调试体验 | 同步调用栈清晰 | async stacktrace 碎片化 |
| 类型安全 | 编译期发现错误 | 运行时才报错 |
| 部署 | 单二进制，Docker 镜像 < 20MB | 依赖地狱，镜像动辄几百 MB |
| 学习价值 | 字节/B站/Google 后端主流 | AI 脚本更多 |

**一个容易忽略的细节：** FastAPI 的 async/await 是"传染性"的。一旦某个底层函数是 async，整条调用链都得变成 async，协程切换的 stacktrace 很难读。Go 的 goroutine 完全对开发者透明，`go func()` 启动，channel 通信，代码和同步逻辑几乎一样写。

**Go 的弱点也要承认：** 错误处理冗余（`if err != nil` 到处写），初期模板代码多。用 Gin 框架可以减少部分，剩下的就当 Go 语言学习的一部分。

### Go 项目目录结构

遵循 golang-standards/project-layout，结合实际业务划分：

```
calliope-api/
├── cmd/server/main.go          # 入口：加载配置、初始化依赖、启动 HTTP server
├── internal/
│   ├── handler/                # HTTP 层：只做参数绑定和响应格式化
│   │   ├── auth.go             # 注册/登录/刷新/登出
│   │   ├── task.go             # 任务创建/查询
│   │   ├── work.go             # 作品管理
│   │   └── websocket.go        # WebSocket 升级
│   ├── service/                # 业务逻辑层：不依赖 HTTP 框架
│   │   ├── auth_service.go
│   │   ├── task_service.go     # 任务创建、回调处理、超时检测
│   │   └── notification_service.go  # WebSocket 连接管理 + Redis Pub/Sub
│   ├── repository/             # 数据访问层：只做 SQL
│   ├── queue/
│   │   └── redis_stream.go     # Redis Stream 生产者
│   └── storage/
│       └── qiniu.go            # 七牛云签名 URL 生成
├── pkg/
│   ├── jwt/                    # JWT 签发/验证
│   └── response/               # 统一响应格式
└── migrations/                 # golang-migrate 迁移文件
```

**分层原则：** handler 不含业务逻辑，service 不知道 HTTP 是什么，repository 不知道业务规则是什么。每层只向下依赖，测试时可以单独 mock 下层。

---

## 决策二：音频分发走七牛云 CDN 直链

### 为什么不让 Go API 中转音频？

最省事的方案是：客户端 → Go API → 从 OSS 拉音频 → 流式返回客户端。

这个方案有一个致命问题：**带宽成本归零变成带宽成本爆炸**。

一首 AI 生成的歌曲约 3-5MB。如果走 Go API 中转：
- 阿里云 ECS 出带宽：约 ¥0.8/GB
- 10 个内测用户每天 5 首 = 50 首 = 250MB/天 ≈ ¥0.2/天，尚可接受
- 但一旦用户增长，带宽成本线性放大

正确做法：**生成签名 URL，让客户端直接访问七牛云 CDN**。

```
Go API 生成 1 小时有效的签名 URL：
https://cdn.calliope-music.com/audio/67/12345/candidate_a.mp3
  ?e=1741510000
  &token={access_key}:{hmac-sha1-sign}

客户端拿到 URL 后直接请求七牛云 CDN，Go API 不参与音频传输。
```

这样做，ECS 出带宽成本归零；流量成本转移到七牛云 CDN 计费侧，有 10GB 永久免费存储额度和一定 CDN 免费流量，MVP 阶段基本可控。随用户增长，成本仍会增加，但单价远低于 ECS 出带宽，且可预期。

### 候选音频的清理

AudioCraft 每次生成两个候选（candidate_a.mp3、candidate_b.mp3），用户选一个保存。未选择的候选音频 24 小时后清理：

```sql
-- 扫描条件：已终态 + 24h 已过 + 候选文件还在
-- 注意：completed_at 对 completed 和 failed 两种终态均写入（schema 注释：'完成或失败时间'），
-- 因此 WHERE completed_at < ... 对两种终态均生效，不存在 failed 任务永久跳过清理的问题。
SELECT id, candidate_a_key, candidate_b_key
FROM tasks
WHERE (status = 'completed' OR status = 'failed')
  AND completed_at < NOW() - INTERVAL 24 HOUR
  AND (candidate_a_key IS NOT NULL OR candidate_b_key IS NOT NULL);
```

---

## 决策三：认证方案

### JWT Access Token + Redis Refresh Token

```
JWT Access Token (AT)：
- 有效期 15 分钟
- 无状态：只验证签名，不查 Redis
- 过期后用 Refresh Token 换新 AT

Redis Refresh Token (RT)：
Key:   calliope:auth:refresh:{user_id}
Value: UUID v4
TTL:   7 天

优势：
- AT 验证无 Redis 查询，性能好
- RT 存 Redis，支持主动撤销（登出只需 DEL key）
- 单用户只保留最新 RT（覆盖写），MVP 不支持多设备
```

**一个常见的错误设计：** 每次刷新 AT 时同时轮换 RT（Refresh Token Rotation）。听起来更安全，但对 MVP 阶段来说增加了复杂度——RT 轮换需要处理并发刷新时的竞态（两个请求同时刷新，后到的 RT 作废，先到的请求拿到的 RT 失效）。当前方案不轮换 RT，简单可靠，登出时 DEL key 即可撤销。

### 暴力破解防护

两层防护：

```
Redis 层（快速拦截）：
  calliope:auth:lock:{email} → 失败次数计数器，TTL 15 分钟
  ≥ 5 次失败 → 拒绝后续登录请求，返回 403

MySQL 层（审计）：
  login_attempts 表记录每次登录（成功/失败）
  可事后分析攻击模式
```

---

## 决策四：额度并发安全

每个用户每天只能生成 5 次，如何在高并发下不超扣？

### 错误方案：先读后写

```go
// 先查还剩几次
used := db.QueryOne("SELECT used FROM credits WHERE user_id=? AND date=?")
if used >= 5 {
    return ErrQuotaFull
}
// 再更新 +1
db.Exec("UPDATE credits SET used=used+1 WHERE user_id=? AND date=?")
```

这个方案在并发下会超扣：两个请求同时读到 `used=4`，都通过了校验，都执行了 +1，最终 `used=6`。

### 正确方案：写即检查（MySQL 行锁）

```sql
INSERT INTO credits (user_id, date, used, limit_count)
VALUES (?, ?, 1, 5)
ON DUPLICATE KEY UPDATE
    used = IF(used < limit_count, used + 1, used);

-- 通过 ROW_COUNT() 判断结果：
-- ROW_COUNT() = 1 → 新行插入（当日第一次），成功
-- ROW_COUNT() = 2 → 已有行且 used 实际 +1，成功
-- ROW_COUNT() = 0 → used >= limit_count，额度已满
--
-- 注意：上述语义依赖 MySQL client flags 中 CLIENT_FOUND_ROWS = false（默认值）。
-- 若 DSN 中加了 clientFoundRows=true，ROW_COUNT() 返回"找到行数"而非"变更行数"，
-- used 未变时也会返回 1，导致额度满检测失效。
-- go-sql-driver/mysql 默认 clientFoundRows=false，切勿在 DSN 中开启此选项。
```

`INSERT ON DUPLICATE KEY UPDATE` 在 MySQL 行锁保护下执行，整个操作原子。ROW_COUNT() 的语义是"受影响的行数"——新插入返回 1，实际更新（值有变化）返回 2，值未变（IF 条件不满足）返回 0。不需要也不应该在这之前再读一次做"预检查"。

**为什么返回 402 而不是 429？** 429 在本系统已用于 IP 限流（`calliope:rate:{ip}`）和队列满（`QUEUE_FULL`），两者都是"请求速率过快，稍后重试"语义。额度耗尽不是速率问题，今天的配额用完后重试也没用，语义更接近"需要付费/充值才能继续"，402 Payment Required 在此更准确。客户端 SDK 将 402 映射为 `BusinessError.insufficientCredits`，与 429 的 `RateLimitError` 在错误处理路径上分开，避免口径混乱。

**为什么不用 Redis 做额度控制？** Redis 的 INCR/DECR 更快，但额度数据需要和 MySQL 的 credits 表保持一致——任务失败时要退还额度，跨零点时要按 `credit_date`（非当前日期）退回。如果主存储在 Redis，退款逻辑和账期对齐会更复杂。MySQL 行锁方案对于每用户每日 5 次的低频操作完全够用。

### credit_date 的坑

tasks 表有一个字段 `credit_date DATE NOT NULL`，记录扣减额度时的 UTC+8 日期。

**为什么不用 `CURDATE()`？**

MySQL 默认 `time_zone=UTC`，凌晨 0:00~8:00 北京时间期间，`CURDATE()` 返回的是"昨天"（UTC 日期）。如果任务在北京时间 0:30 创建，用 `CURDATE()` 扣的是昨天的额度，任务失败时退款也退到昨天，账期错乱。

正确做法：**Go API 侧计算 UTC+8 日期，作为参数传入**：

```go
cst, _ := time.LoadLocation("Asia/Shanghai")
creditDate := time.Now().In(cst).Format("2006-01-02")
// 然后作为参数 ? 传给 SQL，禁止在 SQL 里用 CURDATE()
```

---

## 决策五：WebSocket 多实例扩展

### 问题：Python Worker 回调任意一个 Go API 实例

当部署多个 Go API 实例时，用户 WebSocket 连接在实例 A，但 Python Worker 的 HTTP 回调可能打到实例 B——实例 B 无法推送到实例 A 上的 WebSocket 连接。

### 方案：Redis Pub/Sub 广播

```
Python Worker 完成任务 → HTTP 回调任意一个 Go API 实例
  → Go API 实例（无论哪个）更新 MySQL
  → PUBLISH calliope:ws:task:{task_id} 消息

所有 Go API 实例都订阅了这个 channel：
  → 持有该 task_id WebSocket 连接的实例收到消息
  → 推送到客户端
```

这个方案对 MVP 阶段足够。如果后续连接数达到百万级，可以考虑专门的 WebSocket 网关 + 连接路由，但那是另一个量级的问题。

**Pub/Sub 不持久化的影响：** 如果客户端 WebSocket 断开期间任务完成了，重连后收不到推送消息。解决方案：重连时**先订阅 channel，再调用 `GET /tasks/:id` 检查当前状态**——顺序不能反。若先 GET 再 SUBSCRIBE，GET 返回到 SUBSCRIBE 生效之间如果 PUBLISH 已发出，事件会永久丢失。正确流程：SUBSCRIBE → GET（若已终态则直接处理，否则等推送）。另一个风险：Go API 实例本身的 Redis Pub/Sub 订阅因网络抖动静默断开（WebSocket 连接未断），此时客户端会长期停留旧状态。兜底方案：客户端在 WS 连接正常但超过 60 秒未收到进度更新时启用轮询（不应在 WS 正常推送时也轮询），间隔 30 秒，任务到达终态（completed/failed）后立即停止，最长持续至任务创建后 **90 分钟**（与服务端清理阈值对齐）。90 分钟后若仍未终态，展示"排队超时，请联系支持"，**不引导重试**——此时任务可能仍在队列中，盲目重试会产生重复扣额度；若需重试应先由服务端确认原任务已清理。

**回调幂等与状态机单调性：** Go API 收到 Python Worker 的状态回调时，通过 MySQL `UPDATE ... WHERE status = '期望前态'` 的前置条件保证状态只能单向流转（queued → processing → completed/failed），重复或乱序回调会因 WHERE 条件不匹配而返回 0 行，Go API 返回 409 给 Worker。Worker 的 A-02 中详述了 409 的处理方式（视 reason 字段决定是否 XACK）。

---

## 部署架构：最小成本运行

个人练习项目，预算目标 ¥0~150/月。

```
阿里云 ECS（2C4G，Ubuntu 22.04，约 ¥60-80/月）
└── Docker Compose
    ├── Nginx（TLS 终止，443/80）
    ├── Go API（:8080，ECS 内网）
    └── Redis（:6379，ECS 内网）

阿里云 RDS MySQL（独立托管，内网访问）
七牛云 Kodo OSS + CDN（10GB 永久免费）

AutoDL RTX 3090（按需租用，¥2/小时）
└── Python 推理服务（Docker）
    ├── FastAPI（localhost:8000，不对外暴露）
    ├── Redis Stream Consumer（连接到 ECS Redis）
    └── AudioCraft MusicGen-Small
```

### AutoDL 与阿里云跨厂商通信

AutoDL 和阿里云是不同厂商，无法 VPC 互通，跨厂商通信走公网。安全措施：

1. **Redis**：双路访问——Go API 走 ECS 内网（6379 端口，不对外暴露）；Python Worker 走公网（启用 TLS + requirepass + 防火墙白名单仅放行 AutoDL IP 段）。Redis 需同时监听内网和公网接口，或通过端口映射对外暴露 TLS 端口。
2. **Go API 回调接口**：HTTPS + Bearer 共享密钥（`INTERNAL_CALLBACK_SECRET`）+ X-Timestamp（限制重放时间窗至 60s；两端依赖 NTP 同步，实测时钟偏差 < 1s，60s 窗口有足够余量）。**密钥轮换**需双端（Go API + Python Worker）同步更新环境变量并重新部署，存在短暂 downtime 窗口；密钥泄露后同样走重新部署流程，MVP 阶段可接受。

### GPU 按需启停

推理服务不 24 小时运行。通过检测 `calliope:queue:depth` 计数器（不是 XLEN，原因见 A-02）决定是否保持运行：
- 有任务：保持运行
- 空闲 10 分钟：关闭 GPU 实例

**depth 计数器漂移风险：** Worker 崩溃、回调超时、或 Redis 重启都可能导致 depth 偏大，使 GPU 无法正常降载或误判 QUEUE_FULL。缓解措施：Go API 服务启动时从 MySQL 重算并修正（`SELECT COUNT(*) FROM tasks WHERE status IN ('queued','processing')`），定时超时扫描在标记任务 failed 时也会同步 DECR，长期偏差会自收敛。

按 10 个内测用户、每人 5 次/天估算：约 50 次/天 × 2 分钟/次 = 100 分钟/天 ≈ ¥3.3/天 ≈ ¥100/月。控制在预算内。

**冷启动影响：** GPU 从停止到可用约 1-3 分钟。这段时间发生在 Worker 取到任务之前（`started_at` 之前），不计入推理超时窗口（180s）。MVP 内测阶段无硬性 SLA，前端通过队列位置和进度提示缓解体感。

---

## 总结：关键决策一览

| 决策点 | 选择 | 核心理由 |
|--------|------|---------|
| 服务数量 | 2 个（Go API + Python Worker） | 避免过度拆分；WebSocket 内嵌 goroutine 天然支持 |
| API 层语言 | Go + Gin | 并发模型好；类型安全；学习价值高；部署简单 |
| 任务队列 | Redis Stream | GPU 是瓶颈；复用已有 Redis；Kafka 大材小用 |
| WebSocket 扩展 | Redis Pub/Sub | Worker 回调任意实例，广播到持有连接的实例 |
| 认证 | JWT(15min) + Redis RT(7天) | AT 无状态快；RT 可撤销 |
| 音频分发 | 七牛云 CDN 直链 + 签名 URL | 带宽成本归零；七牛云 10GB 永久免费 |
| 额度并发 | MySQL INSERT ON DUPLICATE KEY UPDATE | 行锁原子写，写即检查，无需 Redis 分布式锁 |
| GPU 方案 | AutoDL 按需租用（RTX 3090，¥2/h） | 国内，支付宝，按秒计费，成本最低 |

---

## 后记

这套架构的设计原则是：**在满足当前需求的前提下，选择最简单的方案**。Kafka 很强大，但 GPU 是瓶颈，每秒入队不超过 10 个，Kafka 完全是大材小用。微服务很优雅，但 WebSocket 内嵌 Go API 更省事。

个人练习项目最大的敌人是过度设计。把精力花在真正有技术深度的地方——额度并发安全、XADD+INCR 的原子性、credit_date 的时区问题——这些细节才是架构能力的体现。

下一篇：[《为什么选 Redis Stream 而不是 Kafka：任务队列选型实战》](./A-02-redis-stream-vs-kafka.md)，深入讲队列选型和实现细节。
