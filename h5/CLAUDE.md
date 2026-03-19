# H5 客户端开发约束

详细规范见 [docs/coding-standards/h5.md](../docs/coding-standards/h5.md)
架构设计见 [docs/architecture/client-h5.md](../docs/architecture/client-h5.md)

## 强制约束

### 模块
- 严格使用 ES Modules（`import/export`），禁止 CommonJS
- import 路径必须带文件扩展名（`.js`），浏览器原生模块要求
- 禁止 `var`，优先 `const`，必要时用 `let`

### Token
- `refresh()` 只写 `accessToken` 和 `expiresAt`，禁止覆盖 `refreshToken`（后端不轮换 RT）
- token 刷新逻辑只在 `storage/token-store.js` 和 `api/client.js` 中处理，其他模块禁止直接操作 localStorage token

### 任务监听
- `watchTask` 在 `api/task.js` 中实现，负责 WS→轮询降级、210s 超时
- `ws/task-socket.js` 只做 per-task WS 连接，不含重连和轮询逻辑
- `startPolling()` 在 `await request()` 返回后必须先检查 `if (cancelled) return;` 再调用 `onUpdate()`

### 音频
- 音频状态枚举：`idle / loading / ready / playing / paused / ended / error`
- `seek(positionSeconds)` 参数单位为秒（非毫秒）
- `audio.play()` 必须在用户手势事件回调中调用（Safari 限制）

### 错误处理
- `client.js` 统一抛出 `APIError`，包含 `code`、`message`、`status` 字段
- 错误码必须与 `docs/architecture/client-sdk-spec.md` 错误码表一致（如 `RATE_LIMIT_EXCEEDED`）
- 禁止裸 `catch (e) {}` 静默吞掉错误

### 测试
- 新功能先写测试再实现（TDD）
- `api/client.js` token 刷新逻辑 100% 覆盖
- `ws/task-socket.js` 100% 覆盖
- 使用 Vitest，禁止依赖真实网络
