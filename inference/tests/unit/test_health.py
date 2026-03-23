"""health 端点单元测试"""

import pytest
from fastapi.testclient import TestClient
from unittest.mock import AsyncMock

from app.config import Settings
from app.main import create_app
from app.worker.task_worker import TaskWorker


@pytest.fixture
def client_with_worker() -> TestClient:
    settings = Settings(
        redis_url="redis://localhost:6379/0",
        go_api_base_url="http://go-api:8080",
        internal_callback_secret="test_secret",
        inference_backend="mock",
        worker_id="worker-test",
    )
    # 不启动 lifespan，手动装配 state
    app = create_app(settings)

    mock_worker = AsyncMock(spec=TaskWorker)
    mock_worker.get_stats.return_value = {
        "stream_length": 3,
        "pending_count": 1,
        "current_task_id": 42,
    }
    app.state.worker = mock_worker

    return TestClient(app, raise_server_exceptions=True)


def test_health(client_with_worker: TestClient) -> None:
    resp = client_with_worker.get("/health")
    assert resp.status_code == 200
    data = resp.json()
    assert data["status"] == "ok"
    assert data["worker_id"] == "worker-test"
    assert data["inference_backend"] == "mock"


def test_queue_stats(client_with_worker: TestClient) -> None:
    resp = client_with_worker.get("/internal/queue/stats")
    assert resp.status_code == 200
    data = resp.json()
    assert data["stream_length"] == 3
    assert data["pending_count"] == 1
    assert data["current_task_id"] == 42
