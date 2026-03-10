# 数据库 Schema 设计

> 版本：v1.0
> 更新日期：2026-03-09

---

## 1. MySQL 表结构

### 1.1 users 表

```sql
CREATE TABLE users (
    id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    email        VARCHAR(100)    NOT NULL,
    password     VARCHAR(60)     NOT NULL COMMENT 'bcrypt hash，固定 60 字符',
    nickname     VARCHAR(50)     NOT NULL DEFAULT '' COMMENT '显示名称，默认空',
    status       TINYINT         NOT NULL DEFAULT 1 COMMENT '1=active, 0=banned',
    created_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    UNIQUE KEY uk_email (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户表';
```

**字段说明：**
- `password`：bcrypt 哈希后固定 60 字符（cost=12）
- `status=0`：账号封禁（暴力破解锁定时间存 Redis，不在此字段）
- MVP 阶段无头像，不存 avatar_url

---

### 1.2 tasks 表

```sql
CREATE TABLE tasks (
    id               BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT,
    user_id          BIGINT UNSIGNED  NOT NULL,
    prompt           VARCHAR(200)     NOT NULL,
    lyrics           TEXT             DEFAULT NULL COMMENT 'NULL 表示 AI 自动生成歌词',
    mode             ENUM('vocal','instrumental') NOT NULL DEFAULT 'vocal',
    status           ENUM('queued','processing','completed','failed')
                                      NOT NULL DEFAULT 'queued',
    fail_reason      VARCHAR(500)     DEFAULT NULL,
    credit_date      DATE             NOT NULL COMMENT '扣减额度时的 UTC+8 日期（应用层传入，非 CURDATE()），退款时按此日期回补',
    queue_position   INT              DEFAULT NULL COMMENT '入队时的位置（前方等待任务数）',
    candidate_a_key  VARCHAR(500)     DEFAULT NULL COMMENT '七牛云文件路径 key，候选 A',
    candidate_b_key  VARCHAR(500)     DEFAULT NULL COMMENT '七牛云文件路径 key，候选 B',
    duration_seconds INT              DEFAULT NULL COMMENT '生成音频时长（秒）',
    inference_ms     INT              DEFAULT NULL COMMENT '推理耗时（毫秒），用于监控',
    started_at       DATETIME         DEFAULT NULL COMMENT '开始推理时间',
    completed_at     DATETIME         DEFAULT NULL COMMENT '完成或失败时间',
    created_at       DATETIME         NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME         NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    KEY idx_user_id_created (user_id, created_at),
    KEY idx_status_started  (status, started_at) COMMENT '定时任务扫描超时任务用（按 started_at 过滤）',
    CONSTRAINT fk_tasks_user FOREIGN KEY (user_id) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='音乐生成任务表';
```

**字段说明：**
- `candidate_a_key` / `candidate_b_key`：存七牛云路径 key（非完整 URL）；完整 URL 在查询时动态签名生成
- `credit_date`：NOT NULL，创建任务时由应用层写入当时的 UTC+8 日期（Go API 侧取 `time.Now().In(cst).Format("2006-01-02")`，禁止用 DB 的 CURDATE()，MySQL 默认 time_zone=UTC 会返回错误账期）；失败退款 SQL 用 `WHERE date = tasks.credit_date`，保证跨零点失败时退回正确账期
- 状态机：`queued → processing → completed / failed`
- 任务超时（3 分钟）：定时任务扫描 `status='processing' AND started_at < NOW()-180s`，更新为 `failed`，退还额度（按 `credit_date`）
- 候选音频在 `completed_at + 24h` 后由定时任务删除，此后将两个 key 置 NULL

---

### 1.3 works 表

```sql
CREATE TABLE works (
    id               BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT,
    user_id          BIGINT UNSIGNED  NOT NULL,
    task_id          BIGINT UNSIGNED  NOT NULL COMMENT '来源任务',
    title            VARCHAR(50)      NOT NULL COMMENT '作品名称',
    prompt           VARCHAR(200)     NOT NULL COMMENT '冗余存储，避免查询作品列表时 JOIN tasks',
    mode             ENUM('vocal','instrumental') NOT NULL,
    audio_key        VARCHAR(500)     NOT NULL COMMENT '七牛云文件路径 key',
    duration_seconds INT              NOT NULL DEFAULT 0,
    play_count       INT UNSIGNED     NOT NULL DEFAULT 0,
    deleted_at       DATETIME         DEFAULT NULL COMMENT '软删除时间',
    created_at       DATETIME         NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME         NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    KEY idx_user_id_created (user_id, created_at),
    UNIQUE KEY uk_task_id (task_id) COMMENT '一个任务只能保存一条作品，数据库层兜底防并发重复写入',
    CONSTRAINT fk_works_user FOREIGN KEY (user_id) REFERENCES users (id),
    CONSTRAINT fk_works_task FOREIGN KEY (task_id) REFERENCES tasks (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户保存的作品表';
```

**字段说明：**
- `prompt` 冗余存储在 works，作品列表查询无需 JOIN tasks
- 删除为软删除（设 `deleted_at`），七牛云文件由后台定时任务批量清理
- `audio_key` 保存用户选择的那一个候选的路径（或将候选文件复制到 `works/` 前缀下）

---

### 1.4 credits 表

```sql
CREATE TABLE credits (
    id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id      BIGINT UNSIGNED NOT NULL,
    date         DATE            NOT NULL COMMENT 'UTC+8 日期，每天零点重置',
    used         TINYINT         NOT NULL DEFAULT 0 COMMENT '已使用次数',
    limit_count  TINYINT         NOT NULL DEFAULT 5 COMMENT '当日上限',
    created_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    UNIQUE KEY uk_user_date (user_id, date),
    CONSTRAINT fk_credits_user FOREIGN KEY (user_id) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='每日生成额度表';
```

**并发安全设计：**

```sql
-- 扣减（利用 MySQL 行锁保证原子性，无需 Redis）
-- 第二个参数传应用层计算好的 UTC+8 日期，如 Go: time.Now().In(cst).Format("2006-01-02")
-- 禁止使用 CURDATE()，MySQL 默认 time_zone=UTC，凌晨 0-8 点会返回错误账期
INSERT INTO credits (user_id, date, used, limit_count)
VALUES (?, ?, 1, 5)
ON DUPLICATE KEY UPDATE
    used = IF(used < limit_count, used + 1, used);

-- 检查是否扣减成功：
--   ROW_COUNT() = 1 → 新行插入（当日第一次），成功
--   ROW_COUNT() = 2 → 已有行且 used 实际 +1，成功
--   ROW_COUNT() = 0 → used >= limit_count，IF 未改变值，额度已满，失败
-- 即：ROW_COUNT() IN (1, 2) 表示成功，ROW_COUNT() = 0 表示额度满

-- 退还（任务失败时）：按 tasks.credit_date 退，而非 CURDATE()，防止跨零点退到错误账期
UPDATE credits SET used = GREATEST(used - 1, 0)
WHERE user_id = ? AND date = ?;  -- 第二个参数传 tasks.credit_date
```

---

### 1.5 login_attempts 表

```sql
CREATE TABLE login_attempts (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    email      VARCHAR(100)    NOT NULL,
    ip         VARCHAR(45)     NOT NULL COMMENT 'IPv4 或 IPv6',
    success    TINYINT(1)      NOT NULL DEFAULT 0,
    created_at DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (id),
    KEY idx_email_created (email, created_at),
    KEY idx_ip_created    (ip, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='登录尝试记录（暴力破解检测）';
```

**使用方式：**
- 查询最近 15 分钟内同一邮箱失败次数 ≥ 5 → 拒绝登录，返回 403
- 登录成功后清除 Redis 锁定计数（见 2.2）
- 定时任务每天清理 24 小时前的记录

---

## 2. Redis 数据结构

### 命名规范

```
{prefix}:{resource}:{identifier}

前缀统一为：calliope

示例：
  calliope:auth:refresh:{user_id}    → Refresh Token
  calliope:auth:lock:{email}         → 登录失败锁定
  calliope:rate:{ip}                 → IP 限流计数
  calliope:ws:task:{task_id}         → WebSocket 通知 Pub/Sub channel
```

---

### 2.1 Refresh Token

```
Key:    calliope:auth:refresh:{user_id}
Type:   String
Value:  <UUID v4>
TTL:    604800 秒（7 天）；remember_me=true 时 2592000 秒（30 天）

操作：
  SET   calliope:auth:refresh:123  "uuid-xxxx"  EX 604800
  GET   calliope:auth:refresh:123               → 验证 token 是否匹配
  DEL   calliope:auth:refresh:123               → 登出/撤销
```

> 单用户只保留最新 Refresh Token（覆盖写），MVP 阶段不支持多设备同时登录。

---

### 2.2 登录失败锁定

```
Key:    calliope:auth:lock:{email}
Type:   String（计数器）
TTL:    900 秒（15 分钟，每次失败重置窗口）

操作：
  INCR   calliope:auth:lock:user@example.com       → 失败次数 +1
  EXPIRE calliope:auth:lock:user@example.com  900  → 重置 15 分钟窗口
  GET    calliope:auth:lock:user@example.com        → ≥ 5 则锁定
  DEL    calliope:auth:lock:user@example.com        → 登录成功后清除
```

---

### 2.3 IP 限流

```
Key:    calliope:rate:{ip}
Type:   String（计数器）
TTL:    60 秒（固定窗口）

操作：
  SET   calliope:rate:1.2.3.4  0  EX 60  NX   → 窗口不存在时初始化
  INCR  calliope:rate:1.2.3.4               → 每次请求 +1
  GET   calliope:rate:1.2.3.4               → ≥ 30 则返回 429
```

---

### 2.4 任务队列（Redis Stream）

```
Stream:  calliope:tasks:stream
Group:   inference-workers
Consumer: worker-{hostname}

消息字段（XADD fields）：
  task_id    → "12345"
  user_id    → "67"
  prompt     → "一首欢快的流行歌曲"
  lyrics     → ""（空字符串表示 AI 自动生成）
  mode       → "vocal"
  created_at → "2026-03-09T10:00:00Z"

生产者（Go API，通过 Lua 脚本原子执行 XADD + INCR calliope:queue:depth）：
  XADD calliope:tasks:stream MAXLEN ~ 10000 * \
       task_id 12345 \
       user_id 67 \
       prompt "一首欢快的流行歌曲" \
       lyrics "" \
       mode vocal \
       created_at "2026-03-09T10:00:00Z"

注意：
  - MAXLEN ~ 10000：近似修剪（~ 表示允许误差，性能友好），保留最近约 10000 条消息。
    XACK 不删除 stream entry，若不设 MAXLEN，stream 会单调增长耗尽 Redis 内存。
  - XADD 和 INCR calliope:queue:depth 合并在同一 Lua 脚本内原子执行（见 system-overview.md §2.1 步骤 5）。

消费者（Python Worker）：
  XREADGROUP GROUP inference-workers worker-001 \
             COUNT 1 BLOCK 5000 \
             STREAMS calliope:tasks:stream >

  处理完成后 ACK：
  XACK calliope:tasks:stream inference-workers {message_id}

  处理失败（推理报错）：不 ACK，消息进入 PEL（Pending Entries List）
  可通过 XCLAIM 重新消费，或超过重试次数后标记任务失败

队列监控：
  XLEN     calliope:tasks:stream                              → 总消息数
  XPENDING calliope:tasks:stream inference-workers - + 10    → 待处理消息
```

---

### 2.5 WebSocket 通知（Pub/Sub）

```
Channel:  calliope:ws:task:{task_id}

发布者（Go API，收到 Python Worker HTTP 回调后）：
  PUBLISH calliope:ws:task:12345 '<JSON 消息>'

订阅者（Go API WebSocket handler，每个任务连接订阅对应 channel）：
  SUBSCRIBE calliope:ws:task:12345

消息格式（JSON）：
{
  "task_id": 12345,
  "status": "queued" | "processing" | "completed" | "failed",
  "queue_position": 2,        // 仅 status=queued 时有值
  "progress": 60,             // 仅 status=processing 时有值，0-100（伪进度）
  "fail_reason": "timeout",   // 仅 status=failed 时有值
  "completed_at": "2026-..."  // 仅 status=completed 时有值
}
```

> Pub/Sub 是非持久化的。如果客户端 WebSocket 断开后重连，应先轮询 `GET /tasks/:id` 获取当前状态，再重新订阅 channel。

---

### 2.6 队列积压深度计数器

```
Key:    calliope:queue:depth
Type:   String（计数器）
TTL:    无（持久）

语义：已提交但尚未到达终态（completed 或 failed）的任务数。
      代表"正在排队或正在推理"的任务总数，是队列门禁的准确依据。

操作：
  INCR  calliope:queue:depth          → 与 XADD 通过 Lua 脚本原子执行（防止进程崩溃导致计数偏低）
  DECR  calliope:queue:depth          → Go API 收到 completed/failed 回调、写 MySQL 成功后执行；定时超时扫描标记 failed 时同样执行
  GET   calliope:queue:depth          → 队列门禁检查（≥ 20 返回 429）

为何不用 XLEN：
  XACK 只从 PEL 移除消息，不删除 stream entry，XLEN 单调增长，不代表当前积压。
  独立计数器精确跟踪"活跃"任务数，与 stream entry 生命周期解耦。

异常恢复（计数器失真时）：
  服务重启或 Redis 重启后，可通过 MySQL 重算：
    SELECT COUNT(*) FROM tasks WHERE status IN ('queued','processing')
  然后 SET calliope:queue:depth {count} 修正。
```

---

## 3. 七牛云文件存储结构

### Bucket 规划

| Bucket | 用途 | 访问权限 |
|--------|------|---------|
| `calliope-audio` | 所有音频文件 | 私有（需签名 URL） |

### 文件路径（Key）规范

```
候选音频（任务生成阶段，24h 后清理）：
  audio/{user_id}/{task_id}/candidate_a.mp3
  audio/{user_id}/{task_id}/candidate_b.mp3

保存的作品（用户选择后，永久保留）：
  works/{user_id}/{work_id}.mp3

示例：
  audio/67/12345/candidate_a.mp3
  works/67/88.mp3
```

### 签名下载 URL

```
基础域名：  https://cdn.calliope-music.com
签名参数：  七牛云私有空间下载签名（e=过期时间戳 &token=AccessKey:HMAC-SHA1）
有效期：    3600 秒（1 小时）

URL 格式：
  https://cdn.calliope-music.com/audio/67/12345/candidate_a.mp3
    ?e=1741510000
    &token={access_key}:{sign}
```

### 候选音频清理策略

- 触发时机：定时任务，每小时执行一次
- 扫描条件：`tasks` 表中 `(status='completed' OR status='failed') AND completed_at < NOW() - INTERVAL 24 HOUR AND (candidate_a_key IS NOT NULL OR candidate_b_key IS NOT NULL)`
- 操作：调用七牛云批量删除 API，删除两个候选文件
- 删除后：将 `tasks.candidate_a_key` 和 `tasks.candidate_b_key` 更新为 NULL
