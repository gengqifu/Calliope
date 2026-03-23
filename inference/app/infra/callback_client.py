"""Go API 回调客户端

协议：POST /internal/tasks/{task_id}/status
认证：Authorization: Bearer {secret} + X-Timestamp（防重放）
重试：指数退避，最多 max_retries 次（由调用方传入，对应 settings.callback_max_retries）
"""

import asyncio
import logging
import time
from typing import Any

import httpx

from app.exceptions import CallbackAuthError, CallbackError

logger = logging.getLogger(__name__)


class CallbackClient:
    def __init__(self, base_url: str, secret: str, max_retries: int = 3) -> None:
        self._base_url = base_url.rstrip("/")
        self._secret = secret
        self._max_retries = max_retries

    async def report_processing(self, task_id: int) -> None:
        await self._send(task_id, {"status": "processing"})

    async def report_completed(
        self,
        task_id: int,
        candidate_a_key: str,
        candidate_b_key: str,
        duration_seconds: int,
        inference_ms: int,
    ) -> None:
        await self._send(
            task_id,
            {
                "status": "completed",
                "candidate_a_key": candidate_a_key,
                "candidate_b_key": candidate_b_key,
                "duration_seconds": duration_seconds,
                "inference_ms": inference_ms,
            },
        )

    async def report_failed(self, task_id: int, fail_reason: str) -> None:
        await self._send(task_id, {"status": "failed", "fail_reason": fail_reason})

    async def _send(self, task_id: int, payload: dict[str, Any]) -> None:
        url = f"{self._base_url}/internal/tasks/{task_id}/status"

        for attempt in range(self._max_retries):
            headers = {
                "Authorization": f"Bearer {self._secret}",
                "X-Timestamp": str(int(time.time())),
            }
            try:
                async with httpx.AsyncClient(timeout=10.0, trust_env=False) as client:
                    resp = await client.post(url, json=payload, headers=headers)

                if resp.status_code == 204:
                    return
                if resp.status_code == 409:
                    # duplicate 或 invalid_transition，视为成功
                    logger.debug(
                        "callback conflict task_id=%d reason=%s",
                        task_id,
                        resp.json().get("reason"),
                    )
                    return
                if resp.status_code == 401:
                    raise CallbackAuthError(
                        f"callback auth failed for task {task_id}", task_id
                    )
                if resp.status_code == 404:
                    # 任务不存在，XACK 丢弃
                    logger.error("callback 404 task_id=%d not found, skipping", task_id)
                    return
                # 5xx 或其他错误，指数退避重试
                logger.warning(
                    "callback http %d task_id=%d attempt=%d",
                    resp.status_code,
                    task_id,
                    attempt,
                )

            except httpx.RequestError as exc:
                logger.warning(
                    "callback network error task_id=%d attempt=%d: %s",
                    task_id,
                    attempt,
                    exc,
                )

            if attempt < self._max_retries - 1:
                await asyncio.sleep(2**attempt)

        raise CallbackError(
            f"callback failed after {self._max_retries} attempts for task {task_id}",
            task_id,
        )
