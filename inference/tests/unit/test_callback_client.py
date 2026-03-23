"""CallbackClient 单元测试"""

import pytest
import respx
import httpx

from app.exceptions import CallbackAuthError, CallbackError
from app.infra.callback_client import CallbackClient


BASE_URL = "http://go-api:8080"
SECRET = "test_secret"


@pytest.fixture
def client() -> CallbackClient:
    return CallbackClient(BASE_URL, SECRET)


@pytest.mark.asyncio
async def test_report_processing_success(client: CallbackClient) -> None:
    with respx.mock:
        route = respx.post(f"{BASE_URL}/internal/tasks/42/status").mock(
            return_value=httpx.Response(204)
        )
        await client.report_processing(42)
    assert route.called


@pytest.mark.asyncio
async def test_report_completed_success(client: CallbackClient) -> None:
    with respx.mock:
        route = respx.post(f"{BASE_URL}/internal/tasks/42/status").mock(
            return_value=httpx.Response(204)
        )
        await client.report_completed(
            task_id=42,
            candidate_a_key="audio/1/42/a.mp3",
            candidate_b_key="audio/1/42/b.mp3",
            duration_seconds=30,
            inference_ms=5000,
        )
    assert route.called
    request = route.calls.last.request
    import json
    body = json.loads(request.content)
    assert body["status"] == "completed"
    assert body["candidate_a_key"] == "audio/1/42/a.mp3"


@pytest.mark.asyncio
async def test_409_treated_as_success(client: CallbackClient) -> None:
    with respx.mock:
        respx.post(f"{BASE_URL}/internal/tasks/42/status").mock(
            return_value=httpx.Response(
                409, json={"code": "CALLBACK_CONFLICT", "reason": "duplicate", "current_status": "processing"}
            )
        )
        # 不应抛出异常
        await client.report_processing(42)


@pytest.mark.asyncio
async def test_401_raises_auth_error(client: CallbackClient) -> None:
    with respx.mock:
        respx.post(f"{BASE_URL}/internal/tasks/42/status").mock(
            return_value=httpx.Response(401)
        )
        with pytest.raises(CallbackAuthError):
            await client.report_processing(42)


@pytest.mark.asyncio
async def test_404_returns_silently(client: CallbackClient) -> None:
    with respx.mock:
        respx.post(f"{BASE_URL}/internal/tasks/42/status").mock(
            return_value=httpx.Response(404)
        )
        # 404 静默返回，不抛出异常
        await client.report_processing(42)


@pytest.mark.asyncio
async def test_500_retries_and_raises(client: CallbackClient) -> None:
    with respx.mock:
        route = respx.post(f"{BASE_URL}/internal/tasks/42/status").mock(
            return_value=httpx.Response(500)
        )
        with pytest.raises(CallbackError):
            await client.report_processing(42)
    # 应重试 3 次
    assert route.call_count == 3


@pytest.mark.asyncio
async def test_network_error_retries_and_raises(client: CallbackClient) -> None:
    with respx.mock:
        route = respx.post(f"{BASE_URL}/internal/tasks/42/status").mock(
            side_effect=httpx.ConnectError("connection refused")
        )
        with pytest.raises(CallbackError):
            await client.report_processing(42)
    assert route.call_count == 3
