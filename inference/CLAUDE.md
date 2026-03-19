# Python 推理服务开发约束

详细规范见 [docs/coding-standards/python.md](../docs/coding-standards/python.md)

## 强制约束

### 类型
- 所有函数必须有完整类型注解（参数 + 返回值）
- 禁止 `Any` 类型（除非确实无法避免，需加注释说明原因）
- 请求/响应模型必须用 Pydantic BaseModel，禁止裸 dict

### 异步
- 所有 IO 操作（HTTP、Redis、OSS）必须 async/await
- AudioCraft 推理（CPU/GPU 密集）必须放 `run_in_executor` 线程池，禁止阻塞 event loop
- 禁止裸 `except Exception: pass`

### 错误处理
- 业务错误用自定义 Exception 类，FastAPI exception_handler 统一格式化返回
- Worker 捕获异常后必须将任务状态更新为 FAILED，禁止静默丢弃

### 测试
- 新功能先写测试再实现（TDD）
- services 层覆盖率 100%
- 使用 `pytest-asyncio`，禁止在 async 测试中用 `asyncio.run()`

### 代码风格
- 提交前必须通过 `black` 格式化和 `ruff` lint
- import 分组：标准库 → 第三方 → 本项目内部
