# 内部服务间通信协议

> 版本：v1.0
> 更新日期：2026-03-09
> 适用服务：Go API 服务 ↔ Python 推理服务

---

## 1. 概述

Go API 服务与 Python 推理服务之间通过两个通道通信：

| 方向 | 通道 | 用途 |
|------|------|------|
| Go API → Python Worker | Redis Stream | 下发推理任务 |
| Python Worker → Go API | HTTPS 回调 | 上报任务状态变更 |

两个服务**不共享数据库**。MySQL 读写只能通过 Go API 进行；Python Worker 不直接访问 MySQL。

---

## 2. Go API → Python Worker：Redis Stream

### 2.1 Stream 配置

```
Stream Key:   calliope:tasks:stream
Consumer Group:   inference-workers
Consumer Name:    worker-{hostname}   （如 worker-autodl-3090-01）
```

### 2.2 消息格式

Go API 使用 `XADD` 写入，Python Worker 使用 `XREADGROUP` 消费。

**消息字段（全部为字符串）：**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `task_id` | string(int64) | 是 | 任务 ID |
| `user_id` | string(int64) | 是 | 用户 ID |
| `prompt` | string | 是 | 生成提示词，1-200 字符 |
| `lyrics` | string | 是 | 歌词，空字符串 `""` 表示 AI 自动生成 |
| `mode` | string | 是 | `vocal` 或 `instrumental` |
| `created_at` | string(RFC3339) | 是 | 任务创建时间，UTC，如 `2026-03-09T10:00:00Z` |

**示例：**

```redis
XADD calliope:tasks:stream MAXLEN ~ 10000 * \
  task_id   "12345" \
  user_id   "67" \
  prompt    "一首欢快的流行歌曲，节奏明快" \
  lyrics    "" \
  mode      "vocal" \
  created_at "2026-03-09T10:00:00Z"
```

> 实际由 Lua 脚本原子执行（同时 INCR `calliope:queue:depth`），此处展示消息格式。`MAXLEN ~ 10000` 防止 stream 单调增长（XACK 不删除 entry）。

### 2.3 消费流程

```python
# Python Worker 伪代码
while True:
    messages = redis.xreadgroup(
        groupname="inference-workers",
        consumername=f"worker-{hostname}",
        streams={"calliope:tasks:stream": ">"},
        count=1,
        block=5000  # 阻塞等待 5 秒
    )
    for stream, entries in messages:
        for msg_id, fields in entries:
            try:
                handle_task(fields)
                redis.xack("calliope:tasks:stream", "inference-workers", msg_id)
            except Exception as e:
                # 不 ACK，消息留在 PEL，等待重试或人工介入
                log.error(f"task {fields['task_id']} failed: {e}")
```

### 2.4 重试与死信处理

- Worker 崩溃恢复后，通过 `XPENDING` 查询 PEL 中未 ACK 的消息，用 `XCLAIM` 重新认领
- 单条消息重试超过 3 次后，Worker 通过 HTTP 回调将任务标记为 `failed`（fail_reason=`inference_error`），然后 `XACK` 丢弃该消息
- 定时任务（Go API 侧）：每分钟扫描 PEL 中超过 10 分钟未 ACK 的消息，对应任务标记为 `failed`（fail_reason=`timeout`），同时执行 `DECR calliope:queue:depth` 和退还额度（按 `tasks.credit_date`）

---

## 3. Python Worker → Go API：HTTP 状态回调

### 3.1 接口

```
POST https://api.calliope-music.com/internal/tasks/{task_id}/status
Authorization: Bearer {INTERNAL_CALLBACK_SECRET}
X-Timestamp: 1741510000          ← Unix 时间戳（秒），必填
Content-Type: application/json
```

**鉴权与安全：**

| 机制 | 说明 |
|------|------|
| Bearer 共享密钥 | `INTERNAL_CALLBACK_SECRET` 两端共享，Go API 做字符串常量时间比较（`subtle.ConstantTimeCompare`），防止时序攻击 |
| 时间窗防重放 | Go API 拒绝 `abs(now - X-Timestamp) > 60s` 的请求，返回 401；网络重试须在 60 秒内完成 |
| 状态前置条件幂等 | Go API 的 UPDATE 语句带 `WHERE status = 'queued'`（processing 回调）或 `WHERE status = 'processing'`（completed/failed 回调），重复回调 UPDATE 影响 0 行 → 返回 409，Worker 视 409 为成功 |

> 以上三层机制分工：Bearer 密钥防止未授权调用；时间戳防止捕获后重放；WHERE 前置条件防止并发或网络重试导致的重复状态写入。

### 3.2 请求体

#### 3.2.1 processing（任务开始推理）

```json
{
  "status": "processing"
}
```

调用时机：Worker 从 Redis Stream 取到消息、开始调用推理引擎之前。

#### 3.2.2 completed（推理完成，文件已上传到七牛云）

```json
{
  "status": "completed",
  "candidate_a_key": "audio/67/12345/candidate_a.mp3",
  "candidate_b_key": "audio/67/12345/candidate_b.mp3",
  "duration_seconds": 30,
  "inference_ms": 45320
}
```

调用时机：两个候选音频均上传到七牛云 OSS 后。

#### 3.2.3 failed（推理失败）

```json
{
  "status": "failed",
  "fail_reason": "inference_error"
}
```

`fail_reason` 枚举值：

| 值 | 含义 |
|----|------|
| `timeout` | 任务超时（由 Go API 定时任务触发，Worker 也可主动上报） |
| `inference_error` | AudioCraft / SiliconFlow 推理报错 |
| `upload_error` | 音频生成成功但上传七牛云失败 |

### 3.3 响应

| HTTP 状态码 | 含义 | Worker 处理 |
|------------|------|------------|
| 204 | 成功 | 继续 |
| 401 | 密钥无效或时间戳超期 | 抛异常，不 XACK，消息留 PEL 由超时扫描兜底；需人工检查 INTERNAL_CALLBACK_SECRET 配置 |
| 404 | task_id 不存在 | 记录 ERROR，正常 return，XACK 丢弃（重试无意义） |
| 409 | 见响应体 `reason` 字段 | 见下表 |
| 5xx | Go API 或网关错误（含 500/502/503/504） | 指数退避重试，耗尽后记录 ERROR，不 XACK |

**409 响应体结构：**

```json
{
  "code": "CALLBACK_CONFLICT",
  "reason": "duplicate" | "invalid_transition",
  "current_status": "processing"
}
```

| reason | 含义 | Worker 处理 |
|--------|------|------------|
| `duplicate` | 相同状态已写入（重复回调） | 视为成功（debug 日志），不重试 |
| `invalid_transition` | 状态前置条件不符（如 completed 后再收到 processing） | 记录 WARN，不重试；可能是乱序或 bug |

Go API 返回 409 时的判断逻辑：
```
UPDATE 影响 0 行后，查询 SELECT status FROM tasks WHERE id=?
  - 当前 status == 目标状态（如 status='processing' 收到 processing 回调）
    → reason="duplicate"
  - 当前 status 是更晚的状态（如 status='completed' 收到 processing 回调）
    → reason="invalid_transition"
```

### 3.4 Go API 收到回调后的处理逻辑

所有 UPDATE 均检查 `ROW_COUNT()`：为 0 表示状态前置条件不满足（重复回调或乱序），返回 409，Worker 视 409 为成功，不重试。

```
收到 processing 回调：
  1. 校验时间戳（abs(now - X-Timestamp) > 60s → 401）
  2. UPDATE tasks SET status='processing', started_at=NOW()
     WHERE id=? AND status='queued'
     → ROW_COUNT()=0 → return 409（已处理，幂等）
  3. PUBLISH calliope:ws:task:{task_id} {"status":"processing","progress":0}
  4. return 204

收到 completed 回调：
  1. 校验时间戳
  2. UPDATE tasks SET status='completed', candidate_a_key=?, candidate_b_key=?,
                      duration_seconds=?, inference_ms=?, completed_at=NOW()
     WHERE id=? AND status='processing'
     → ROW_COUNT()=0 → return 409
  3. DECR calliope:queue:depth（队列深度计数器 -1）
  4. PUBLISH calliope:ws:task:{task_id} {"status":"completed","completed_at":"..."}
  5. return 204

收到 failed 回调：
  1. 校验时间戳
  2. UPDATE tasks SET status='failed', fail_reason=?, completed_at=NOW()
     WHERE id=? AND status IN ('queued','processing')
     → ROW_COUNT()=0 → return 409（任务已结束，无需退款）
  3. 退还额度（按 tasks.credit_date，而非 CURDATE()，避免跨零点失败退到错误账期）：
     UPDATE credits SET used=GREATEST(used-1,0)
     WHERE user_id=? AND date=tasks.credit_date
  4. DECR calliope:queue:depth（队列深度计数器 -1）
  5. PUBLISH calliope:ws:task:{task_id} {"status":"failed","fail_reason":"..."}
  6. return 204
```

### 3.5 回调失败重试（Python Worker 侧）

```python
def callback_go_api(task_id: int, payload: dict, max_retries: int = 3):
    for attempt in range(max_retries):
        try:
            resp = httpx.post(
                f"{GO_API_BASE_URL}/internal/tasks/{task_id}/status",
                json=payload,
                headers={
                    "Authorization": f"Bearer {INTERNAL_CALLBACK_SECRET}",
                    "X-Timestamp": str(int(time.time())),  # 每次重试刷新时间戳
                },
                timeout=10.0
            )
            if resp.status_code == 204:
                return  # 成功

            if resp.status_code == 409:
                body = resp.json()
                reason = body.get("reason")
                if reason == "duplicate":
                    log.debug(f"task {task_id} duplicate callback, treating as success")
                    return
                else:  # invalid_transition
                    log.warning(f"task {task_id} invalid_transition: current_status={body.get('current_status')}")
                    return  # 不重试，可能是乱序或 bug

            if resp.status_code == 401:
                # raise：密钥错误或时钟漂移导致时间戳超期（>60s），需人工介入
                # 排障方向：① 检查 INTERNAL_CALLBACK_SECRET 两端是否一致；② 检查 Worker 系统时钟是否与 NTP 同步
                # 任务不可 XACK，留 PEL 等超时扫描兜底
                raise RuntimeError(f"task {task_id} callback auth failed (check secret or clock skew)")

            if resp.status_code == 404:
                log.error(f"task {task_id} not found in Go API")
                return  # 任务不存在，XACK 丢弃（重试无意义）

            if resp.status_code >= 500:
                # 5xx（含 500/502/503/504）：可能是瞬时抖动，与网络异常同级重试
                # 不重试代价：等待最长 10 分钟 PEL 超时；重试代价：最多额外 7 秒
                if attempt == max_retries - 1:
                    log.error(f"task {task_id} callback got {resp.status_code} after {max_retries} retries")
                    raise RuntimeError(f"callback server error {resp.status_code}")
                log.warning(f"task {task_id} callback got {resp.status_code}, retrying (attempt {attempt+1})")
                time.sleep(2 ** attempt)
                continue

            # 未知状态码：raise，保守处理，不 XACK
            raise RuntimeError(f"task {task_id} callback unexpected status {resp.status_code}")

        except httpx.RequestError as e:
            # 覆盖所有网络层错误：TimeoutException、ConnectError、TLS、DNS 等
            if attempt == max_retries - 1:
                log.error(f"task {task_id} callback failed after {max_retries} retries: {e}")
                raise
            time.sleep(2 ** attempt)  # 指数退避：1s, 2s, 4s
```

> **回调结果到 XACK 决策映射（上层任务处理器参考）：**
>
> | 回调结果 | callback_go_api 行为 | 上层 XACK？ | 理由 |
> |---------|---------------------|-----------|------|
> | 204 成功 | return | **是** | 正常完成 |
> | 409 duplicate | return | **是** | 状态已正确写入，重复确认 |
> | 409 invalid_transition | return | **是** | 任务已在终态，消息无价值 |
> | 404 任务不存在 | return | **是** | 重试无意义 |
> | 401 密钥错误 | **raise** | **否** | 留 PEL，人工介入或等超时扫描 |
> | 5xx 重试耗尽 | **raise** | **否** | 留 PEL，等 Go API 超时扫描接管 |
> | 网络错误耗尽 | **raise** | **否** | 留 PEL，等 Go API 超时扫描接管 |
> | 未知状态码 | **raise** | **否** | 保守处理，留 PEL |
>
> **规则：`callback_go_api` 正常 return → 上层 XACK；抛异常 → 上层不 XACK，消息留 PEL。**
> Go API 超时扫描（10 分钟）通过 XCLAIM 接管 PEL 中的消息，将任务标记为 `failed` 并执行退款和 DECR。
> §2.4 中"推理重试超过 3 次后发 `failed` 回调再 XACK"是**推理失败**路径，与回调网络失败路径相互独立。

---

## 4. Python Worker 健康检查接口

供 Go API 或运维监控查询 Worker 状态。

```
GET http://localhost:8000/health        （AutoDL 本机访问）
GET http://localhost:8000/internal/queue/stats
```

**健康检查响应：**

```json
{
  "status": "ok",
  "worker_id": "worker-autodl-3090-01",
  "model_loaded": true,
  "inference_backend": "musicgen"
}
```

**队列统计响应：**

```json
{
  "stream_length": 3,
  "pending_count": 1,
  "current_task_id": 12345,
  "current_task_started_at": "2026-03-09T10:05:00Z"
}
```

---

## 5. 环境变量清单

### Go API 侧

| 变量名 | 说明 | 示例 |
|--------|------|------|
| `REDIS_URL` | Redis 连接串（含密码） | `rediss://:password@1.2.3.4:6379/0` |
| `INTERNAL_CALLBACK_SECRET` | 内部回调共享密钥 | `<随机生成 64 字符高熵字符串>` |

### Python Worker 侧

| 变量名 | 说明 | 示例 |
|--------|------|------|
| `REDIS_URL` | Redis 连接串（同上） | `rediss://:password@1.2.3.4:6379/0` |
| `GO_API_BASE_URL` | Go API 公网地址 | `https://api.calliope-music.com` |
| `INTERNAL_CALLBACK_SECRET` | 内部回调共享密钥（与 Go API 相同） | `<同上>` |
| `INFERENCE_BACKEND` | 推理后端选择 | `musicgen` 或 `siliconflow` |
| `QINIU_ACCESS_KEY` | 七牛云 AK | — |
| `QINIU_SECRET_KEY` | 七牛云 SK | — |
| `QINIU_BUCKET` | 存储桶名 | `calliope-audio` |

> `INTERNAL_CALLBACK_SECRET` 应通过 `openssl rand -hex 32` 生成，不得使用弱密码。
