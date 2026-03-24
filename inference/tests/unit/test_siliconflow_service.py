"""SiliconFlowInferenceService 单元测试（HTTP 调用用 respx mock）"""

from unittest.mock import AsyncMock

import httpx
import pytest
import respx

from app.exceptions import InferenceError, StorageError
from app.models.schemas import TaskMessage
from app.services.siliconflow_service import SiliconFlowInferenceService

BASE_URL = "https://api.siliconflow.cn"
GENERATE_URL = f"{BASE_URL}/v1/audio/generations"


@pytest.fixture
def mock_storage() -> AsyncMock:
    storage = AsyncMock()
    storage.upload_audio.side_effect = lambda key, data, **kw: key
    return storage


@pytest.fixture
def svc(mock_storage: AsyncMock) -> SiliconFlowInferenceService:
    return SiliconFlowInferenceService(
        storage=mock_storage,
        api_key="test_key",
        base_url=BASE_URL,
        model="stable-audio",
        timeout_seconds=10,
    )


@pytest.fixture
def task() -> TaskMessage:
    return TaskMessage(
        task_id=42,
        user_id=1,
        prompt="upbeat electronic music",
        mode="instrumental",
        created_at="2026-03-23T10:00:00Z",
    )


@pytest.mark.asyncio
async def test_generate_binary_response(
    svc: SiliconFlowInferenceService, task: TaskMessage
) -> None:
    """API 直接返回 audio bytes 时正常生成两个候选"""
    with respx.mock:
        respx.post(GENERATE_URL).mock(
            return_value=httpx.Response(
                200,
                content=b"fake_mp3_bytes",
                headers={"content-type": "audio/mpeg"},
            )
        )
        result = await svc.generate(task)

    assert result.candidate_a_key == "audio/1/42/candidate_a.mp3"
    assert result.candidate_b_key == "audio/1/42/candidate_b.mp3"
    assert result.duration_seconds == 30
    assert result.inference_ms >= 0


@pytest.mark.asyncio
async def test_generate_json_url_response(
    svc: SiliconFlowInferenceService, task: TaskMessage
) -> None:
    """API 返回 JSON + CDN URL 时，先下载再上传"""
    with respx.mock:
        respx.post(GENERATE_URL).mock(
            return_value=httpx.Response(
                200,
                json={"url": "https://cdn.example.com/audio.mp3"},
            )
        )
        respx.get("https://cdn.example.com/audio.mp3").mock(
            return_value=httpx.Response(200, content=b"downloaded_mp3_bytes")
        )
        result = await svc.generate(task)

    assert result.candidate_a_key.endswith("candidate_a.mp3")
    assert result.candidate_b_key.endswith("candidate_b.mp3")


@pytest.mark.asyncio
async def test_generate_calls_api_twice(
    svc: SiliconFlowInferenceService, task: TaskMessage
) -> None:
    """生成两个候选，API 被调用两次（不同 seed）"""
    call_seeds: list[int] = []

    with respx.mock:
        def capture_seed(request: httpx.Request) -> httpx.Response:
            body = request.content
            import json
            payload = json.loads(body)
            call_seeds.append(payload["seed"])
            return httpx.Response(
                200, content=b"fake_mp3", headers={"content-type": "audio/mpeg"}
            )

        respx.post(GENERATE_URL).mock(side_effect=capture_seed)
        await svc.generate(task)

    assert len(call_seeds) == 2
    assert call_seeds[0] != call_seeds[1]


@pytest.mark.asyncio
async def test_generate_401_raises_inference_error(
    svc: SiliconFlowInferenceService, task: TaskMessage
) -> None:
    """API 返回 401 时抛出 InferenceError，task_id 与任务一致"""
    with respx.mock:
        respx.post(GENERATE_URL).mock(return_value=httpx.Response(401))
        with pytest.raises(InferenceError) as exc_info:
            await svc.generate(task)
    assert exc_info.value.task_id == task.task_id


@pytest.mark.asyncio
async def test_generate_429_raises_inference_error(
    svc: SiliconFlowInferenceService, task: TaskMessage
) -> None:
    """API 返回 429 时抛出 InferenceError，task_id 与任务一致"""
    with respx.mock:
        respx.post(GENERATE_URL).mock(return_value=httpx.Response(429))
        with pytest.raises(InferenceError) as exc_info:
            await svc.generate(task)
    assert exc_info.value.task_id == task.task_id


@pytest.mark.asyncio
async def test_generate_5xx_raises_inference_error(
    svc: SiliconFlowInferenceService, task: TaskMessage
) -> None:
    """API 返回 5xx 时抛出 InferenceError，task_id 与任务一致"""
    with respx.mock:
        respx.post(GENERATE_URL).mock(return_value=httpx.Response(503))
        with pytest.raises(InferenceError) as exc_info:
            await svc.generate(task)
    assert exc_info.value.task_id == task.task_id


@pytest.mark.asyncio
async def test_generate_storage_error_propagates(
    svc: SiliconFlowInferenceService, task: TaskMessage, mock_storage: AsyncMock
) -> None:
    """OSS 上传失败时 StorageError 正常向上传播"""
    mock_storage.upload_audio.side_effect = StorageError("oss error", task.task_id)

    with respx.mock:
        respx.post(GENERATE_URL).mock(
            return_value=httpx.Response(
                200, content=b"mp3", headers={"content-type": "audio/mpeg"}
            )
        )
        with pytest.raises(StorageError):
            await svc.generate(task)
