"""MockInferenceService 单元测试"""

import pytest

from app.models.schemas import TaskMessage
from app.services.inference import MockInferenceService


@pytest.fixture
def task() -> TaskMessage:
    return TaskMessage(
        task_id=42,
        user_id=1,
        prompt="electronic beats",
        mode="instrumental",
        created_at="2026-03-23T10:00:00Z",
    )


@pytest.mark.asyncio
async def test_mock_inference_returns_result(task: TaskMessage) -> None:
    svc = MockInferenceService()
    result = await svc.generate(task)

    assert result.candidate_a_key == "audio/1/42/candidate_a.mp3"
    assert result.candidate_b_key == "audio/1/42/candidate_b.mp3"
    assert result.duration_seconds == 30
    assert result.inference_ms > 0


@pytest.mark.asyncio
async def test_mock_inference_key_includes_user_and_task(task: TaskMessage) -> None:
    svc = MockInferenceService()
    result = await svc.generate(task)

    assert "1" in result.candidate_a_key  # user_id
    assert "42" in result.candidate_a_key  # task_id
