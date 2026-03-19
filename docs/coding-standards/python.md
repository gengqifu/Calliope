# Python 编码规范

> 适用范围：`inference/` 目录下所有 Python 代码

---

## 工具链

| 工具 | 版本 | 用途 |
|--|--|--|
| Python | 3.11+ | 语言运行时 |
| black | 24.x | 代码格式化（强制，不可配置风格）|
| isort | 5.x | import 排序 |
| ruff | 0.4+ | Lint（替代 flake8 + pylint）|
| mypy | 1.x | 静态类型检查 |
| pytest | 8.x | 测试框架 |
| pytest-asyncio | 0.23+ | async 测试支持 |

### 配置文件（pyproject.toml）

```toml
[tool.black]
line-length = 88
target-version = ["py311"]

[tool.isort]
profile = "black"

[tool.ruff]
line-length = 88
select = ["E", "F", "I", "N", "UP", "ANN"]
ignore = ["ANN101", "ANN102"]

[tool.mypy]
python_version = "3.11"
strict = true
ignore_missing_imports = true

[tool.pytest.ini_options]
asyncio_mode = "auto"
testpaths = ["tests"]
```

---

## 目录结构规范

```
inference/
├── app/
│   ├── main.py             # FastAPI app 初始化和路由注册
│   ├── api/
│   │   └── v1/
│   │       └── tasks.py    # HTTP endpoint（只做请求解析和响应）
│   ├── services/           # 业务逻辑
│   │   └── inference.py
│   ├── worker/             # Redis Stream 消费 Worker
│   │   └── task_worker.py
│   ├── models/             # Pydantic 请求/响应模型
│   │   └── task.py
│   ├── infra/              # 基础设施（Redis、OSS 客户端）
│   │   ├── redis.py
│   │   └── storage.py
│   └── config.py           # 配置（pydantic-settings）
├── tests/
│   ├── unit/
│   └── integration/
├── requirements.txt
├── requirements-dev.txt
└── pyproject.toml
```

---

## 类型注解规范

### 所有函数必须有完整类型注解

```python
# ✅ 正确
async def create_task(
    task_id: str,
    prompt: str,
    duration: int = 30,
) -> TaskResult:
    ...

# ❌ 错误：缺少注解
async def create_task(task_id, prompt, duration=30):
    ...
```

### Pydantic 模型（替代 dataclass）

```python
from pydantic import BaseModel, Field

class TaskRequest(BaseModel):
    task_id: str = Field(..., description="任务 ID")
    prompt: str = Field(..., min_length=1, max_length=500)
    duration: int = Field(default=30, ge=10, le=300)

class TaskResult(BaseModel):
    task_id: str
    audio_url: str
    duration_seconds: float
```

---

## 异步规范

```python
# ✅ 所有 IO 操作使用 async/await
async def fetch_task(task_id: str) -> Task:
    async with httpx.AsyncClient() as client:
        resp = await client.get(f"/tasks/{task_id}")
        resp.raise_for_status()
        return Task(**resp.json())

# ✅ CPU 密集任务（AudioCraft 推理）放线程池
import asyncio
from concurrent.futures import ThreadPoolExecutor

executor = ThreadPoolExecutor(max_workers=2)

async def run_inference(prompt: str) -> bytes:
    loop = asyncio.get_event_loop()
    return await loop.run_in_executor(executor, _sync_inference, prompt)

def _sync_inference(prompt: str) -> bytes:
    # AudioCraft 同步调用
    ...
```

---

## 错误处理规范

```python
# ✅ 业务错误用自定义异常
class InferenceError(Exception):
    def __init__(self, message: str, task_id: str) -> None:
        super().__init__(message)
        self.task_id = task_id

# ✅ FastAPI 异常处理器统一返回格式
from fastapi import Request
from fastapi.responses import JSONResponse

@app.exception_handler(InferenceError)
async def inference_error_handler(
    request: Request, exc: InferenceError
) -> JSONResponse:
    return JSONResponse(
        status_code=500,
        content={"code": "INFERENCE_ERROR", "message": str(exc)},
    )

# ❌ 禁止裸 except
try:
    result = await run_inference(prompt)
except Exception:  # ❌
    pass
```

---

## 命名规范

| 类别 | 规范 | 示例 |
|--|--|--|
| 模块/文件 | snake_case | `task_worker.py` |
| 类 | PascalCase | `TaskWorker` |
| 函数/变量 | snake_case | `process_task()` |
| 常量 | UPPER_SNAKE | `MAX_RETRY_COUNT = 3` |
| 私有成员 | 单下划线前缀 | `_redis_client` |

---

## 测试规范

```python
# ✅ async 测试（pytest-asyncio）
import pytest

@pytest.mark.asyncio
async def test_run_inference_success(mock_audiocraft):
    result = await run_inference("jazz music, 30s")
    assert result.duration_seconds > 0

# ✅ fixture 复用
@pytest.fixture
def task_request() -> TaskRequest:
    return TaskRequest(task_id="test-001", prompt="jazz music")

# ✅ 集成测试连接真实 Redis（Docker）
@pytest.fixture(scope="session")
async def redis_client():
    client = await aioredis.from_url("redis://localhost:6379")
    yield client
    await client.close()
```

### 覆盖率要求

- 整体 > 70%
- `services/` 层 100%
- `worker/` 层 > 80%

---

## import 分组顺序

```python
# 1. 标准库
import asyncio
from typing import Optional

# 2. 第三方库
import torch
from fastapi import FastAPI
from pydantic import BaseModel

# 3. 本项目内部
from app.config import settings
from app.models.task import TaskRequest
```

`isort --profile black` 自动维护。
