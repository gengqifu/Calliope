# 为什么选 Redis Stream 而不是 Kafka：任务队列选型实战

> 本文是 Calliope AI 音乐生成系统系列文章的第二篇。上篇讲了整体架构选型，这篇专注于任务队列：为什么用 Redis Stream，Kafka 哪里过度，以及 Redis Stream 的关键实现细节（消费者组、消息确认、积压深度计数器、持久化）。

---

## 问题背景

Calliope 的音乐生成是一个典型的**异步长任务**场景：

- 用户提交"生成音乐"请求 → 立即返回（不能让用户等 3 分钟）
- 任务进入队列 → Python Worker 取任务 → AudioCraft 推理（30s ~ 3min）→ 结果写 OSS → 通知用户

这需要一个任务队列。候选方案：Kafka、RabbitMQ、Redis Stream、Redis List。

---

## 为什么 Kafka 是大材小用

### 先看吞吐量数字

| 组件 | 峰值吞吐 |
|------|---------|
| AudioCraft RTX 3090 单任务耗时 | 30s ~ 3min |
| 单 GPU 并发任务上限 | 1~2 个（显存决定） |
| 系统实际入队速率峰值 | < 10 个/秒 |
| Redis Stream 处理能力 | 10 万+/秒 |
| Kafka 处理能力 | 百万+/秒 |

**结论：GPU 是瓶颈，不是队列。** 即使有 4 张 GPU，并发任务上限也只有 8 个左右。在这个场景下，Redis Stream 的能力已经是实际需求的 1 万倍，Kafka 的优势根本用不上。

### Kafka 的实际代价

引入 Kafka 意味着：

1. **JVM 运行时**：Kafka 依赖 Java，独立 JVM 进程吃掉 1-2GB 内存
2. **ZooKeeper（或 KRaft）**：早期版本依赖 ZooKeeper，新版本用 KRaft 也要额外配置
3. **本地开发环境**：`docker compose up` 时 Kafka 容器启动通常需要 30 秒以上，还有端口、配置负担
4. **生产运维**：分区数、副本数、retention policy、consumer lag 监控……一套新的运维体系

对个人项目来说，这些成本是纯开销，换来的能力完全不需要。

---

## Redis Stream：刚好够用的正确选择

### 已经有 Redis 了

Calliope 已经用 Redis 做：
- Refresh Token 存储（`calliope:auth:refresh:{user_id}`）
- 登录失败锁定计数（`calliope:auth:lock:{email}`）
- IP 限流（`calliope:rate:{ip}`）

**复用 Redis = 零新增组件 = 零新增运维负担。**

### Redis Stream 的关键能力

Redis 5.0 引入的 Stream 数据结构，原生支持：

| 能力 | 实现方式 |
|------|---------|
| 消息持久化 | 配置 AOF（appendfsync=everysec/always） |
| 消费者组 | `XREADGROUP`，多 Worker 竞争消费，不重复 |
| 消息确认 | `XACK`，确认后从 PEL 移除 |
| 失败重试 | PEL（Pending Entries List）机制，未 ACK 消息可重新消费 |
| 超时扫描 | `XCLAIM` 将长时间未 ACK 的消息转给其他 Worker |

这五个能力完全覆盖了 Calliope 的需求。

---

## 实现细节：生产者（Go API）

### 入队操作

```go
// queue/redis_stream.go
func (q *RedisStreamQueue) Enqueue(ctx context.Context, task *TaskMessage) (string, error) {
    // 用 Lua 脚本保证 XADD 和 INCR depth 的原子性
    script := redis.NewScript(`
        local id = redis.call('XADD', KEYS[1], 'MAXLEN', '~', '10000', '*',
            'task_id',    ARGV[1],
            'user_id',    ARGV[2],
            'prompt',     ARGV[3],
            'lyrics',     ARGV[4],
            'mode',       ARGV[5],
            'created_at', ARGV[6])
        redis.call('INCR', KEYS[2])
        return id
    `)
    return script.Run(ctx, q.client,
        []string{"calliope:tasks:stream", "calliope:queue:depth"},
        task.TaskID, task.UserID, task.Prompt, task.Lyrics, task.Mode, task.CreatedAt,
    ).Text()
}
```

### 为什么要 Lua 脚本原子化？

`XADD` 和 `INCR calliope:queue:depth` 单独执行时存在风险：

```
XADD 成功 → 进程崩溃 → INCR 没执行 → 计数器偏低
```

计数器偏低会导致队列门禁失效，超过 20 个任务仍然放行入队。虽然对于 MVP 阶段这是软限制（可接受），但用 Lua 脚本两行代码就能消除这个风险，没有理由不做。

### 为什么要 MAXLEN ~ 10000？

一个容易忽略的陷阱：**XACK 不删除 Stream entry。**

`XACK` 只是把消息从 PEL（Pending Entries List）中移除，消息本身还留在 Stream 里。如果不设 MAXLEN，Stream 会单调增长，最终耗尽 Redis 内存。

```
XADD calliope:tasks:stream MAXLEN ~ 10000 * field1 val1 ...
```

`~` 表示"近似"修剪，Redis 会在内部节点边界进行截断，性能比精确 `MAXLEN 10000` 好很多。对于 Calliope 这个场景，保留最近约 10000 条消息完全够用（同时活跃任务最多 20 个，历史消息只用于审计）。

---

## 实现细节：消费者（Python Worker）

### 消费主循环

```python
# worker/stream_consumer.py

STREAM_KEY = "calliope:tasks:stream"
GROUP_NAME = "inference-workers"
CONSUMER_NAME = f"worker-{socket.gethostname()}"

def consume_loop():
    # 确保消费者组存在（幂等）
    try:
        redis_client.xgroup_create(STREAM_KEY, GROUP_NAME, id="0", mkstream=True)
    except ResponseError as e:
        if "BUSYGROUP" not in str(e):
            raise

    last_pel_check = 0
    pel_cursor = "0-0"  # 跨轮保持游标，大积压时分批回收

    while True:
        # PEL 回收：每 60 秒检查一次，XAUTOCLAIM 将超时未 ACK 的消息转给自己重新处理
        # 处理死消费者（进程崩溃）遗留在 PEL 中的任务
        now = time.time()
        if now - last_pel_check > 60:
            # 分页推进 PEL，但设单轮时间预算（5s），超时则记录游标留给下次继续
            # 避免大积压时占用主循环，延迟新消息处理
            pel_deadline = now + 5
            while time.time() < pel_deadline:
                # XAUTOCLAIM 返回 (next_start_id, messages, deleted_ids)
                # next_start_id 是游标，messages 才是消息列表
                next_cursor, claimed, _ = redis_client.xautoclaim(
                    STREAM_KEY, GROUP_NAME, CONSUMER_NAME,
                    min_idle_time=300_000,  # 5 分钟未 ACK 视为死任务
                    start_id=pel_cursor,
                    count=10,
                )
                for message_id, fields in claimed:
                    handle_task(message_id, fields)
                if next_cursor == b"0-0" or next_cursor == "0-0":
                    pel_cursor = "0-0"  # 本轮扫完，下次从头
                    break
                pel_cursor = next_cursor  # 记录游标，下轮继续
            last_pel_check = now

        messages = redis_client.xreadgroup(
            GROUP_NAME, CONSUMER_NAME,
            {STREAM_KEY: ">"},   # ">" = 只取未分配的新消息
            count=1,
            block=5000,          # 阻塞等待 5 秒
        )
        if not messages:
            continue

        for stream, entries in messages:
            for message_id, fields in entries:
                handle_task(message_id, fields)
```

> **注意：** `">"` 只拉取从未分配过的新消息，不会重领 PEL 中的挂起消息。`XAUTOCLAIM`（Redis 6.2+）负责定期将长时间未 ACK 的消息（死消费者遗留）转给当前消费者重新处理，两条路径各司其职，缺少任一都会导致任务积压。

### 单任务处理与 XACK 决策

这里最关键的设计原则：**return = XACK，raise = 不 XACK**。

```python
def handle_task(message_id: str, fields: dict):
    try:
        task = parse_task(fields)

        # 1. 通知 Go API：status=processing
        callback_go_api(task.task_id, {"status": "processing"})

        # 2. AudioCraft 推理
        audio_paths = run_inference(task)

        # 3. 上传到七牛云
        keys = upload_to_qiniu(task, audio_paths)

        # 4. 通知 Go API：status=completed
        callback_go_api(task.task_id, {"status": "completed", **keys})

        # 正常完成 → XACK（从 PEL 移除）
        redis_client.xack(STREAM_KEY, GROUP_NAME, message_id)

    except Exception as e:
        # 异常 → 不 XACK → 消息留在 PEL → 等待超时扫描或重试
        log.error(f"Task {fields.get('task_id')} failed: {e}")
        # 不调用 xack，此处 return
```

### 回调 Go API 的重试策略

```python
def callback_go_api(task_id: int, payload: dict, max_retries: int = 3):
    for attempt in range(max_retries):
        try:
            resp = httpx.post(
                f"{GO_API_BASE}/internal/tasks/{task_id}/status",
                json=payload,
                headers={
                    "Authorization": f"Bearer {INTERNAL_CALLBACK_SECRET}",
                    "X-Timestamp": str(int(time.time())),
                },
                timeout=10,
            )

            if resp.status_code == 204:
                return  # 成功

            if resp.status_code == 409:
                body = resp.json()
                if body.get("reason") == "duplicate":
                    log.debug(f"Duplicate callback for task {task_id}, skip")
                    return  # 幂等，正常返回 → 会 XACK
                else:
                    log.warning(f"Unexpected 409 conflict: {body}")
                    return  # 其他冲突，也 XACK 避免永久卡死

            if resp.status_code == 401:
                # 密钥配置错误，抛异常 → 不 XACK → 人工介入
                raise RuntimeError(f"Internal callback auth failed (401): {resp.text}")

            if resp.status_code == 404:
                # 任务不存在（异常情况），记录 error 但 XACK 避免永久重试
                log.error(f"Task {task_id} not found in Go API (404)")
                return

            if resp.status_code >= 500:
                if attempt == max_retries - 1:
                    raise RuntimeError(f"Go API 5xx after {max_retries} retries: {resp.status_code}")
                log.warning(f"Go API 5xx, retry {attempt + 1}/{max_retries}")
                time.sleep(2 ** attempt)  # 指数退避
                continue

            raise RuntimeError(f"Unexpected status {resp.status_code}")

        except httpx.RequestError as e:
            if attempt == max_retries - 1:
                raise
            time.sleep(2 ** attempt)
```

### XACK 决策一览

| 场景 | 处理 | XACK？ | 理由 |
|------|------|--------|------|
| 正常完成（204） | return | ✅ | 正常终态 |
| 重复回调（409 duplicate） | return | ✅ | 幂等，已处理 |
| 任务不存在（404） | log error + return | ✅ | 避免永久卡死 |
| 推理异常 | raise | ❌ | 留 PEL，待重试或人工介入 |
| 鉴权失败（401） | raise RuntimeError | ❌ | 配置错误，需人工介入 |
| Go API 5xx（重试耗尽） | raise | ❌ | Go API 故障，待恢复后重试 |
| 网络异常（重试耗尽） | raise | ❌ | 网络故障，待恢复后重试 |

---

## 实现细节：队列深度计数器

### 为什么不用 XLEN？

```
XLEN calliope:tasks:stream → 100
```

这个 100 是什么意思？是"有 100 个任务在排队"吗？

不是。**XACK 只从 PEL 移除消息，不删除 Stream entry。** 即使所有任务都已经完成并 ACK，XLEN 还是单调递增的（直到 MAXLEN 截断）。XLEN 不代表当前积压。

### 独立计数器：calliope:queue:depth

```
Key:  calliope:queue:depth
Type: String（计数器）
语义: 已提交但尚未到达终态（completed 或 failed）的任务数

INCR：与 XADD 通过 Lua 脚本原子执行（入队时）
DECR：Go API 收到 completed/failed 回调后执行
DECR：定时超时扫描标记 failed 时执行
```

Go API 队列门禁检查：

```go
depth, err := redisClient.Get(ctx, "calliope:queue:depth").Int64()
if err == nil && depth >= 20 {
    c.JSON(http.StatusTooManyRequests, gin.H{
        "code":    "QUEUE_FULL",
        "message": "服务繁忙，请稍后重试",
    })
    return
}
```

### 计数器失真时的恢复

Redis 重启后（未持久化或 AOF 损坏），计数器会丢失，从 0 开始。服务启动时检测并从 MySQL 重算：

```go
// 服务启动时执行
func (s *TaskService) RecoverQueueDepth(ctx context.Context) error {
    var count int64
    err := s.db.QueryRowContext(ctx,
        "SELECT COUNT(*) FROM tasks WHERE status IN ('queued','processing')",
    ).Scan(&count)
    if err != nil {
        return err
    }
    return s.redis.Set(ctx, "calliope:queue:depth", count, 0).Err()
}
```

---

## 任务超时扫描

Worker 崩溃或推理超时（> 3 分钟）时，任务会卡在 `status=processing`。定时任务每分钟扫描一次：

```go
func (s *TaskService) ScanTimeoutTasks(ctx context.Context) {
    rows, err := s.db.QueryContext(ctx, `
        SELECT id, credit_date, user_id
        FROM tasks
        WHERE status = 'processing'
          AND started_at < NOW() - INTERVAL 180 SECOND
    `)
    // ...
    for rows.Next() {
        var task TimeoutTask
        if err := rows.Scan(&task.ID, &task.CreditDate, &task.UserID); err != nil {
            log.Error("timeout scan row scan failed", "err", err)
            continue
        }

        // 1. CAS 门闩：只有仍在 processing 才能转 failed
        //    若回调线程已置 completed，affected=0，跳过后续步骤，不退款不 DECR
        result, err := s.db.ExecContext(ctx,
            "UPDATE tasks SET status='failed', fail_reason='timeout', completed_at=NOW(), credit_refunded=0 WHERE id=? AND status='processing'",
            task.ID,
        )
        if err != nil {
            log.Error("timeout scan update failed", "task_id", task.ID, "err", err)
            continue
        }
        affected, _ := result.RowsAffected()
        if affected == 0 {
            // 已被并发转为其他状态（completed/failed），跳过
            continue
        }

        // 2. 退还额度 + 标记 credit_refunded=1，放在同一事务保证原子性
        //    若失败，credit_refunded 保持 0，由独立对账任务 ScanUnrefundedTasks
        //    （扫 status='failed' AND credit_refunded=0）补执行；
        //    注意：此处 ScanTimeoutTasks 本身只扫 status='processing'，
        //    步骤 1 成功后任务已变 failed，不会被本扫描器再次命中。
        refundOK := false
        tx, err := s.db.BeginTx(ctx, nil)
        if err != nil {
            log.Error("refund tx begin failed", "task_id", task.ID, "err", err)
            // credit_refunded=0，ScanUnrefundedTasks 负责兜底
        } else {
            _, err = tx.ExecContext(ctx,
                "UPDATE credits SET used=GREATEST(used-1, 0) WHERE user_id=? AND date=?",
                task.UserID, task.CreditDate,
            )
            if err == nil {
                _, err = tx.ExecContext(ctx,
                    "UPDATE tasks SET credit_refunded=1 WHERE id=?", task.ID)
            }
            if err != nil {
                tx.Rollback()
                log.Error("refund tx exec failed", "task_id", task.ID, "err", err)
            } else if commitErr := tx.Commit(); commitErr != nil {
                log.Error("refund tx commit failed", "task_id", task.ID, "err", commitErr)
                // commit 失败，credit_refunded 仍为 0，ScanUnrefundedTasks 负责重试
            } else {
                refundOK = true
            }
        }
        _ = refundOK  // 当前仅用于日志语义，后续可按需加指标上报

        // 3. DECR 深度计数器（步骤 1 成功才执行）
        if err := s.redis.Decr(ctx, "calliope:queue:depth").Err(); err != nil {
            // DECR 失败会导致计数偏高，下次启动时 RecoverQueueDepth 会重算修正
            log.Error("queue depth DECR failed, will self-correct on restart", "task_id", task.ID, "err", err)
        }

        // 4. 推送 WebSocket 通知
        s.NotifyTaskFailed(ctx, task.ID, "timeout")
    }
}
```

**为什么退款要用 `tasks.credit_date` 而不是 `CURDATE()`？**

凌晨 0:00 北京时间，任务是昨天创建的，扣了昨天的额度。如果任务超时在今天被扫描到，用 `CURDATE()` 退款会退到今天的额度，昨天的额度永远不退。`credit_date` 字段记录了创建时的 UTC+8 日期，确保退款到正确账期。

---

## 持久化：AOF 不可省略

Redis Stream 消息默认存在内存里，重启会丢失。**Calliope 必须配置 AOF 以显著降低丢失概率。**

```conf
# redis.conf
appendonly yes
appendfsync everysec   # 每秒 fsync，最多丢失约 1 秒数据（非零丢失）
# appendfsync always   # 每次写都 fsync，接近零丢失但性能低约 10 倍
```

仅配置 RDB（快照）是不够的：RDB 是间歇性快照，宕机前最近一批写入可能丢失。对于任务队列，丢消息意味着用户的生成请求消失，且用户不会知道。

MVP 阶段选 `appendfsync everysec`：持久化窗口约 1 秒，在实际场景中（单用户每天 5 次、低并发）丢失概率极低，性能也远好于 `always`。严格意义上 `everysec` 不能保证"零丢失"，若需要绝对可靠应改用 `always` 或在 MySQL 侧落库兜底。

---

## 与 Kafka 的迁移路径

如果未来规模增长到真的需要 Kafka（日活百万级、多下游消费者组），迁移时需要面对：

| 迁移项 | 工作量 |
|--------|--------|
| Go API 生产端：替换 Lua+XADD 为 kafka-go | 中等 |
| Python Worker 消费端：替换 XREADGROUP 为 confluent-kafka | 中等 |
| ACK 语义对齐：Kafka offset commit vs XACK | 较高（语义不同） |
| PEL 超时扫描替换：Kafka consumer group rebalance | 较高 |
| 深度计数器：需要重新实现（Kafka consumer lag） | 中等 |
| 运维体系：新增 Kafka 集群监控、分区管理 | 较高 |

**不要低估这个迁移成本。** Go API 层通过 `QueueProducer` 接口抽象有一定帮助，但生产/消费基础设施、重试语义、PEL 扫描逻辑都需要同步替换，远不是"改一个文件"的事。

在迁移真正必要之前，Redis Stream 完全可以支撑。

---

## 总结

选 Redis Stream 还是 Kafka，核心问题只有一个：**你的瓶颈是什么？**

Calliope 的瓶颈是 GPU，不是队列吞吐量。单 GPU 并发任务不超过 2 个，Redis Stream 的处理能力是实际需求的 1 万倍。引入 Kafka 换来的是 1-2GB 内存占用、30 秒启动时间、一套新的运维体系，以及一个复杂的迁移路径——这些全是纯开销。

**Redis Stream 选型的技术实现要点：**

1. **XADD + INCR depth 用 Lua 原子化**，防止计数器偏低
2. **MAXLEN ~ 10000**，防止 Stream 单调增长耗尽内存
3. **AOF 持久化**（appendfsync=everysec），显著降低丢失概率（约 1 秒窗口，MVP 可接受）
4. **独立深度计数器**（`calliope:queue:depth`），而不是 XLEN
5. **return = XACK，raise = 不 XACK**，异常留 PEL 等待重试
6. **超时扫描**按 `credit_date` 退款，避免跨零点账期错乱

这六个细节，每一个都踩过坑。

---

上一篇：[《从零设计 AI 音乐生成系统：架构选型与高并发方案》](./A-01-system-architecture-design.md)
