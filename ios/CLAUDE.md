# iOS 客户端开发约束

详细规范见 [docs/coding-standards/ios.md](../docs/coding-standards/ios.md)
架构设计见 [docs/architecture/client-ios.md](../docs/architecture/client-ios.md)

## 强制约束

### 架构
- ViewModel 必须标注 `@MainActor`，属性用 `@Published private(set)` 暴露（禁止外部写入）
- ViewController 只做 UI 绑定和事件转发，禁止包含业务逻辑
- Repository 必须定义 protocol，ViewModel 依赖 protocol 而非具体实现

### 并发
- 所有 IO 操作用 `async/await`，禁止 `DispatchQueue` 做业务并发
- `TokenRefresher` 必须是 `actor`，防止并发重复刷新
- 网络回调更新 UI 必须用 `Task { @MainActor in ... }`

### 资源管理
- `AudioPlayerManager.release()` 必须移除 `AVPlayerItemDidPlayToEndTime` 通知观察者
- 长生命周期 Task 必须在 `deinit` 中调用 `.cancel()`
- `load()` 每次调用前先移除旧的通知观察者，再添加新的（防止累积）

### 错误处理
- 禁止 `try!` 和 `force unwrap`（`!`），除非有注释说明绝对安全的理由
- 业务错误用自定义 enum Error，禁止裸字符串传递错误信息
- ViewModel 统一 catch 并更新 uiState，禁止在 ViewController 处理业务错误

### 测试
- 新功能先写测试再实现（TDD）
- ViewModel 层覆盖率 100%
- 使用 Mock Protocol 实现替换真实依赖
