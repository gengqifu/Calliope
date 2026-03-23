"""Redis Stream 任务消费 Worker

消费流程：
1. XREADGROUP 阻塞读取消息
2. 回调 Go API: status=processing
3. 调用推理服务生成音频
4. 回调 Go API: status=completed（含 OSS key）
5. XACK 确认消息

失败处理：
- 消息格式错误（缺字段/类型错误）→ 记录 ERROR，XACK 丢弃（永久无效，留 PEL 无意义）
- 推理/上传失败 → 回调 status=failed，XACK 丢弃
- 回调 401 → 不 XACK，消息留 PEL，等待人工介入
- 回调 5xx 重试耗尽 → 不 XACK，消息留 PEL
"""

import asyncio
import logging
from typing import Any

from pydantic import ValidationError
from redis.asyncio import Redis

from app.exceptions import CallbackAuthError, CallbackError, InferenceError, StorageError
from app.infra.callback_client import CallbackClient
from app.models.schemas import InferenceResult, TaskMessage
from app.services.inference import InferenceService

logger = logging.getLogger(__name__)


def _parse_task_message(fields: dict[str, str]) -> TaskMessage:
    return TaskMessage(
        task_id=int(fields["task_id"]),
        user_id=int(fields["user_id"]),
        prompt=fields["prompt"],
        mode=fields["mode"],
        created_at=fields["created_at"],
    )


class TaskWorker:
    def __init__(
        self,
        redis: Redis,  # type: ignore[type-arg]
        callback: CallbackClient,
        inference: InferenceService,
        stream_key: str = "calliope:tasks:stream",
        consumer_group: str = "inference-workers",
        worker_id: str = "worker-local",
        block_ms: int = 5000,
    ) -> None:
        self._redis = redis
        self._callback = callback
        self._inference = inference
        self._stream_key = stream_key
        self._consumer_group = consumer_group
        self._worker_id = worker_id
        self._block_ms = block_ms
        self._current_task_id: int | None = None

    @property
    def current_task_id(self) -> int | None:
        return self._current_task_id

    async def _ensure_group(self) -> None:
        try:
            await self._redis.xgroup_create(
                self._stream_key, self._consumer_group, id="0", mkstream=True
            )
        except Exception as exc:
            # BUSYGROUP 说明 group 已存在，忽略
            if "BUSYGROUP" not in str(exc):
                raise

    async def _handle_message(
        self, msg_id: str, fields: dict[str, str]
    ) -> bool:
        """
        处理单条消息。
        返回 True 表示可以 XACK，False 表示不 XACK（留 PEL）。
        """
        try:
            task: TaskMessage = _parse_task_message(fields)
        except (KeyError, ValueError, ValidationError) as exc:
            # 格式错误的消息永远无法处理，XACK 丢弃，循环继续
            logger.error(
                "malformed message msg_id=%s fields=%s error=%s, discarding",
                msg_id,
                fields,
                exc,
            )
            return True

        self._current_task_id = task.task_id
        logger.info("task_id=%d start processing", task.task_id)

        try:
            await self._callback.report_processing(task.task_id)
        except CallbackAuthError:
            # 认证失败，不 XACK，需要人工介入
            logger.error("task_id=%d callback auth failed, leaving in PEL", task.task_id)
            return False
        except CallbackError:
            logger.error(
                "task_id=%d callback processing failed after retries, leaving in PEL",
                task.task_id,
            )
            return False

        fail_reason: str | None = None
        result: InferenceResult | None = None

        try:
            result = await self._inference.generate(task)
        except (InferenceError, Exception) as exc:
            fail_reason = "inference_error"
            logger.error("task_id=%d inference error: %s", task.task_id, exc)

        if result is None:
            # 推理失败，回调 failed 后 XACK
            try:
                await self._callback.report_failed(
                    task.task_id, fail_reason or "inference_error"
                )
            except CallbackAuthError:
                logger.error(
                    "task_id=%d callback auth failed on report_failed, leaving in PEL",
                    task.task_id,
                )
                return False
            except CallbackError:
                logger.error(
                    "task_id=%d callback failed on report_failed, leaving in PEL",
                    task.task_id,
                )
                return False
            return True

        try:
            await self._callback.report_completed(
                task_id=task.task_id,
                candidate_a_key=result.candidate_a_key,
                candidate_b_key=result.candidate_b_key,
                duration_seconds=result.duration_seconds,
                inference_ms=result.inference_ms,
            )
        except CallbackAuthError:
            # 认证失败，不 XACK，需要人工介入
            logger.error(
                "task_id=%d callback auth failed on report_completed, leaving in PEL",
                task.task_id,
            )
            return False
        except CallbackError:
            # 回调重试耗尽，不 XACK，留 PEL 等待重试
            logger.error(
                "task_id=%d callback failed on report_completed, leaving in PEL",
                task.task_id,
            )
            return False
        except Exception as exc:
            # 意外异常（非回调失败），上报 upload_error 后 XACK
            logger.error("task_id=%d report_completed unexpected error: %s", task.task_id, exc)
            try:
                await self._callback.report_failed(task.task_id, "upload_error")
            except (CallbackAuthError, CallbackError):
                return False
            return True

        logger.info("task_id=%d completed", task.task_id)
        return True

    async def run(self) -> None:
        """主消费循环，随 FastAPI lifespan 启动"""
        await self._ensure_group()
        logger.info(
            "worker %s started, stream=%s group=%s",
            self._worker_id,
            self._stream_key,
            self._consumer_group,
        )

        while True:
            try:
                messages: Any = await self._redis.xreadgroup(
                    groupname=self._consumer_group,
                    consumername=self._worker_id,
                    streams={self._stream_key: ">"},
                    count=1,
                    block=self._block_ms,
                )
            except asyncio.CancelledError:
                logger.info("worker %s stopped", self._worker_id)
                break
            except Exception as exc:
                logger.error("xreadgroup error: %s, retrying in 2s", exc)
                await asyncio.sleep(2)
                continue

            if not messages:
                continue

            for _stream, entries in messages:
                for msg_id, fields in entries:
                    should_ack = await self._handle_message(msg_id, fields)
                    if should_ack:
                        await self._redis.xack(
                            self._stream_key, self._consumer_group, msg_id
                        )
                    self._current_task_id = None

    async def get_stats(self) -> dict[str, Any]:
        """返回队列统计（用于 /internal/queue/stats 接口）"""
        stream_len: int = await self._redis.xlen(self._stream_key)
        pending_info: Any = await self._redis.xpending(
            self._stream_key, self._consumer_group
        )
        pending_count: int = pending_info["pending"] if pending_info else 0
        return {
            "stream_length": stream_len,
            "pending_count": pending_count,
            "current_task_id": self._current_task_id,
        }
