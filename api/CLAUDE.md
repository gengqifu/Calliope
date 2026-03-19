# Go API 服务开发约束

详细规范见 [docs/coding-standards/go.md](../docs/coding-standards/go.md)
架构设计见 [docs/architecture/system-overview.md](../docs/architecture/system-overview.md)

## 强制约束

### 分层
- handler 层禁止直接操作数据库或 Redis
- service 层依赖 repository interface，禁止依赖具体实现
- 跨层依赖方向：handler → service → repository → infra

### 错误处理
- 所有 error 必须用 `fmt.Errorf("context: %w", err)` 包裹后向上传递
- 禁止在 handler 层直接返回内部错误信息，统一通过 error middleware 处理
- 禁止裸 `errors.New`（无法包裹原始错误）

### 并发
- 所有 goroutine 必须有明确的退出机制（context 取消或 channel 关闭）
- 禁止 `time.Sleep` 做轮询，用 ticker 或 channel

### 测试
- 新功能先写测试再实现（TDD）
- service 层覆盖率 100%，整体 > 70%
- 集成测试使用真实 DB/Redis（Docker），禁止 mock 数据库

### 代码风格
- 提交前必须通过 `gofmt` 和 `go vet`
- import 分组：标准库 → 第三方 → 本项目内部
