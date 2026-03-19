# Android 客户端开发约束

详细规范见 [docs/coding-standards/android.md](../docs/coding-standards/android.md)
架构设计见 [docs/architecture/client-android.md](../docs/architecture/client-android.md)

## 强制约束

### 架构
- ViewModel 只暴露 `StateFlow`（用 `asStateFlow()`），禁止暴露 `MutableStateFlow`
- Fragment 只做 UI 渲染和事件转发，禁止包含任何业务逻辑
- 数据获取统一通过 Repository，禁止 ViewModel 直接调用 API Service

### 协程
- ViewModel 必须用 `viewModelScope`，禁止 `GlobalScope`
- Fragment 收集 Flow 必须用 `repeatOnLifecycle(STARTED)`
- 捕获异常时必须先 `catch (e: CancellationException) { throw e }`，再处理其他异常
- 禁止裸 `Thread { }.start()`

### 网络
- Token 刷新由 `TokenAuthenticator`（OkHttp）统一处理，业务层禁止手动处理 401
- `isLastErrorNetwork` 只包含 `CONNECTION_FAILED` 和 `CONNECTION_TIMEOUT`，`BAD_HTTP_STATUS` 不属于网络错误

### 状态
- `playWork()` 刷新 audioUrl 时，必须保存完整 Work 对象到 `_currentWork.value`，不能只更新 URL

### 测试
- 新功能先写测试再实现（TDD）
- ViewModel 层覆盖率 100%
- 使用 `TestCoroutineDispatcher` 和 `runTest`

### 依赖注入
- 所有依赖通过 Hilt 注入，禁止手动 `new` 依赖对象
