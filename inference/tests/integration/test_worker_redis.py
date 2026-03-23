"""Worker + Redis Stream 集成测试（需要本地 Redis）

标记为 integration，默认不在 CI 中运行，需手动指定：
    pytest tests/integration/ -m integration
"""

import asyncio
from unittest.mock import AsyncMock

import pytest
import redis.asyncio as aioredis

from app.infra.callback_client import CallbackClient
from app.models.schemas import InferenceResult
from app.services.inference import MockInferenceService
from app.worker.task_worker import TaskWorker

STREAM_KEY = "calliope:test:stream"
GROUP = "test-group"


@pytest.fixture(scope="module")
async def real_redis() -> aioredis.Redis:  # type: ignore[type-arg]
    r = aioredis.from_url("redis://localhost:6379/0", decode_responses=True)
    yield r
    # 清理测试 stream
    await r.delete(STREAM_KEY)
    await r.aclose()


@pytest.mark.asyncio
@pytest.mark.integration
async def test_worker_consumes_message_and_acks(real_redis: aioredis.Redis) -> None:  # type: ignore[type-arg]
    mock_callback = AsyncMock(spec=CallbackClient)
    mock_inference = AsyncMock(spec=MockInferenceService)
    mock_inference.generate.return_value = InferenceResult(
        candidate_a_key="audio/1/99/a.mp3",
        candidate_b_key="audio/1/99/b.mp3",
        duration_seconds=30,
        inference_ms=100,
    )

    worker = TaskWorker(
        redis=real_redis,
        callback=mock_callback,
        inference=mock_inference,
        stream_key=STREAM_KEY,
        consumer_group=GROUP,
        worker_id="worker-integration",
        block_ms=500,
    )
    await worker._ensure_group()

    # 推入一条消息
    await real_redis.xadd(
        STREAM_KEY,
        {
            "task_id": "99",
            "user_id": "1",
            "prompt": "test",
            "mode": "instrumental",
            "created_at": "2026-03-23T10:00:00Z",
        },
    )

    # 运行 Worker，处理完一条消息后取消
    async def run_once() -> None:
        messages = await real_redis.xreadgroup(
            groupname=GROUP,
            consumername="worker-integration",
            streams={STREAM_KEY: ">"},
            count=1,
            block=500,
        )
        if messages:
            for _stream, entries in messages:
                for msg_id, fields in entries:
                    should_ack = await worker._handle_message(msg_id, fields)
                    if should_ack:
                        await real_redis.xack(STREAM_KEY, GROUP, msg_id)

    await run_once()

    # 验证回调被调用
    mock_callback.report_processing.assert_awaited_once_with(99)
    mock_callback.report_completed.assert_awaited_once()

    # 验证消息已 ACK（PEL 为空）
    pending = await real_redis.xpending(STREAM_KEY, GROUP)
    assert pending["pending"] == 0
