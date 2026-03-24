"""pytest fixtures"""

from unittest.mock import AsyncMock

import pytest
import fakeredis.aioredis as fakeredis
from fastapi.testclient import TestClient

from app.config import Settings
from app.infra.callback_client import CallbackClient
from app.infra.storage import StorageClient
from app.main import create_app
from app.models.schemas import TaskMessage
from app.services.inference import MockInferenceService
from app.worker.task_worker import TaskWorker


@pytest.fixture
def settings() -> Settings:
    return Settings(
        redis_url="redis://localhost:6379/0",
        go_api_base_url="http://go-api:8080",
        internal_callback_secret="test_secret",
        inference_backend="mock",
        worker_id="worker-test",
    )


@pytest.fixture
async def fake_redis() -> fakeredis.FakeRedis:
    r = fakeredis.FakeRedis(decode_responses=True)
    yield r
    await r.aclose()


@pytest.fixture
def task_message() -> TaskMessage:
    return TaskMessage(
        task_id=42,
        user_id=1,
        prompt="electronic beats",
        mode="instrumental",
        created_at="2026-03-23T10:00:00Z",
    )


@pytest.fixture
def mock_inference() -> MockInferenceService:
    return MockInferenceService()


@pytest.fixture
def mock_storage() -> AsyncMock:
    """可复用的 StorageClient mock（spec 绑定，upload_audio 返回 key）"""
    storage = AsyncMock(spec=StorageClient)
    storage.upload_audio.side_effect = lambda key, data, **kw: key
    return storage


@pytest.fixture
def test_client(settings: Settings) -> TestClient:
    app = create_app(settings)
    # lifespan 不在 TestClient 中自动触发，手动设置 state
    app.state.settings = settings
    return TestClient(app, raise_server_exceptions=True)
