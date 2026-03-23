"""TaskWorker 单元测试（fakeredis + mock callback）"""

import asyncio
from unittest.mock import AsyncMock, MagicMock

import pytest

from app.exceptions import CallbackAuthError, CallbackError, InferenceError
from app.infra.callback_client import CallbackClient
from app.models.schemas import InferenceResult, TaskMessage
from app.services.inference import MockInferenceService
from app.worker.task_worker import TaskWorker, _parse_task_message


# ---------- _parse_task_message ----------


def test_parse_task_message() -> None:
    fields = {
        "task_id": "42",
        "user_id": "1",
        "prompt": "jazz",
        "mode": "vocal",
        "created_at": "2026-03-23T10:00:00Z",
    }
    msg = _parse_task_message(fields)
    assert msg.task_id == 42
    assert msg.user_id == 1
    assert msg.mode == "vocal"


def test_parse_task_message_missing_field_raises() -> None:
    fields = {"task_id": "42", "user_id": "1", "prompt": "jazz"}  # 缺 mode / created_at
    with pytest.raises(KeyError):
        _parse_task_message(fields)


def test_parse_task_message_invalid_int_raises() -> None:
    fields = {
        "task_id": "not_an_int",
        "user_id": "1",
        "prompt": "jazz",
        "mode": "vocal",
        "created_at": "2026-03-23T10:00:00Z",
    }
    with pytest.raises(ValueError):
        _parse_task_message(fields)


def test_parse_task_message_invalid_mode_raises() -> None:
    from pydantic import ValidationError

    fields = {
        "task_id": "42",
        "user_id": "1",
        "prompt": "jazz",
        "mode": "bad_mode",  # 不符合 pattern
        "created_at": "2026-03-23T10:00:00Z",
    }
    with pytest.raises(ValidationError):
        _parse_task_message(fields)


# ---------- Worker fixtures ----------


@pytest.fixture
def mock_callback() -> AsyncMock:
    cb = AsyncMock(spec=CallbackClient)
    return cb


@pytest.fixture
def mock_inference() -> AsyncMock:
    inf = AsyncMock(spec=MockInferenceService)
    inf.generate.return_value = InferenceResult(
        candidate_a_key="audio/1/42/a.mp3",
        candidate_b_key="audio/1/42/b.mp3",
        duration_seconds=30,
        inference_ms=2000,
    )
    return inf


@pytest.fixture
def worker(fake_redis: MagicMock, mock_callback: AsyncMock, mock_inference: AsyncMock) -> TaskWorker:
    return TaskWorker(
        redis=fake_redis,
        callback=mock_callback,
        inference=mock_inference,
        stream_key="calliope:tasks:stream",
        consumer_group="test-group",
        worker_id="worker-test",
        block_ms=100,
    )


FIELDS = {
    "task_id": "42",
    "user_id": "1",
    "prompt": "jazz",
    "mode": "vocal",
    "created_at": "2026-03-23T10:00:00Z",
}


# ---------- 格式错误消息：XACK 丢弃，不调回调 ----------


@pytest.mark.asyncio
async def test_handle_message_missing_field_discards(
    worker: TaskWorker, mock_callback: AsyncMock
) -> None:
    bad_fields = {"task_id": "42"}  # 缺必填字段
    should_ack = await worker._handle_message("1-0", bad_fields)

    assert should_ack is True  # XACK 丢弃
    mock_callback.report_processing.assert_not_awaited()


@pytest.mark.asyncio
async def test_handle_message_invalid_int_discards(
    worker: TaskWorker, mock_callback: AsyncMock
) -> None:
    bad_fields = {
        "task_id": "not_an_int",
        "user_id": "1",
        "prompt": "jazz",
        "mode": "vocal",
        "created_at": "2026-03-23T10:00:00Z",
    }
    should_ack = await worker._handle_message("1-0", bad_fields)

    assert should_ack is True
    mock_callback.report_processing.assert_not_awaited()


@pytest.mark.asyncio
async def test_handle_message_invalid_mode_discards(
    worker: TaskWorker, mock_callback: AsyncMock
) -> None:
    bad_fields = {
        "task_id": "42",
        "user_id": "1",
        "prompt": "jazz",
        "mode": "bad_mode",
        "created_at": "2026-03-23T10:00:00Z",
    }
    should_ack = await worker._handle_message("1-0", bad_fields)

    assert should_ack is True
    mock_callback.report_processing.assert_not_awaited()


# ---------- 成功流程 ----------


@pytest.mark.asyncio
async def test_handle_message_success(worker: TaskWorker, mock_callback: AsyncMock, mock_inference: AsyncMock) -> None:
    should_ack = await worker._handle_message("1-0", FIELDS)

    assert should_ack is True
    mock_callback.report_processing.assert_awaited_once_with(42)
    mock_inference.generate.assert_awaited_once()
    mock_callback.report_completed.assert_awaited_once()
    assert worker.current_task_id == 42  # 在 handle_message 返回前不清零


# ---------- 推理失败 ----------


@pytest.mark.asyncio
async def test_handle_message_inference_error(
    worker: TaskWorker, mock_callback: AsyncMock, mock_inference: AsyncMock
) -> None:
    mock_inference.generate.side_effect = InferenceError("model crash", 42)

    should_ack = await worker._handle_message("1-0", FIELDS)

    assert should_ack is True
    mock_callback.report_failed.assert_awaited_once_with(42, "inference_error")
    mock_callback.report_completed.assert_not_awaited()


# ---------- 回调 401：不 XACK ----------


@pytest.mark.asyncio
async def test_handle_message_processing_auth_error(
    worker: TaskWorker, mock_callback: AsyncMock
) -> None:
    mock_callback.report_processing.side_effect = CallbackAuthError("auth failed", 42)

    should_ack = await worker._handle_message("1-0", FIELDS)

    assert should_ack is False


# ---------- 回调 5xx 重试耗尽：不 XACK ----------


@pytest.mark.asyncio
async def test_handle_message_processing_callback_error(
    worker: TaskWorker, mock_callback: AsyncMock
) -> None:
    mock_callback.report_processing.side_effect = CallbackError("server error", 42)

    should_ack = await worker._handle_message("1-0", FIELDS)

    assert should_ack is False


# ---------- report_completed 回调失败：不 XACK ----------


@pytest.mark.asyncio
async def test_handle_message_completed_callback_error_leaves_in_pel(
    worker: TaskWorker, mock_callback: AsyncMock, mock_inference: AsyncMock
) -> None:
    mock_callback.report_completed.side_effect = CallbackError("server error", 42)

    should_ack = await worker._handle_message("1-0", FIELDS)

    assert should_ack is False
    mock_callback.report_failed.assert_not_awaited()


@pytest.mark.asyncio
async def test_handle_message_completed_auth_error_leaves_in_pel(
    worker: TaskWorker, mock_callback: AsyncMock, mock_inference: AsyncMock
) -> None:
    mock_callback.report_completed.side_effect = CallbackAuthError("auth failed", 42)

    should_ack = await worker._handle_message("1-0", FIELDS)

    assert should_ack is False
    mock_callback.report_failed.assert_not_awaited()


# ---------- report_completed 意外异常：上报 upload_error 后 XACK ----------


@pytest.mark.asyncio
async def test_handle_message_completed_unexpected_error_reports_upload_error(
    worker: TaskWorker, mock_callback: AsyncMock, mock_inference: AsyncMock
) -> None:
    mock_callback.report_completed.side_effect = RuntimeError("unexpected")

    should_ack = await worker._handle_message("1-0", FIELDS)

    assert should_ack is True
    mock_callback.report_failed.assert_awaited_once_with(42, "upload_error")


# ---------- get_stats ----------


@pytest.mark.asyncio
async def test_get_stats(worker: TaskWorker, fake_redis: MagicMock) -> None:
    # 创建 consumer group 并推入一条消息
    try:
        await fake_redis.xgroup_create(
            "calliope:tasks:stream", "test-group", id="0", mkstream=True
        )
    except Exception:
        pass
    await fake_redis.xadd("calliope:tasks:stream", FIELDS)

    stats = await worker.get_stats()
    assert stats["stream_length"] == 1
    assert stats["pending_count"] == 0
    assert stats["current_task_id"] is None
