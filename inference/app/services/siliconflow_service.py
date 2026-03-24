"""SiliconFlow HTTP API 推理服务（无 GPU 时的 fallback）"""

import logging
import time

import httpx

from app.exceptions import InferenceError, StorageError
from app.infra.storage import StorageClient
from app.models.schemas import InferenceResult, TaskMessage

logger = logging.getLogger(__name__)

# SiliconFlow 音乐生成接口路径（待官方文档确认后更新）
_GENERATE_PATH = "/v1/audio/generations"


class SiliconFlowInferenceService:
    """调用 SiliconFlow API 生成音乐，不依赖本地 GPU"""

    def __init__(
        self,
        storage: StorageClient,
        api_key: str,
        base_url: str = "https://api.siliconflow.cn",
        model: str = "stable-audio",
        timeout_seconds: int = 120,
    ) -> None:
        self._storage = storage
        self._api_key = api_key
        self._base_url = base_url.rstrip("/")
        self._model = model
        self._timeout = timeout_seconds

    async def generate(self, task: TaskMessage) -> InferenceResult:
        """生成两个候选音频，上传到 OSS，返回推理结果"""
        start_ms = int(time.monotonic() * 1000)
        base_seed = task.task_id * 1000

        try:
            audio_a = await self._call_api(task.prompt, base_seed, task.task_id)
            audio_b = await self._call_api(task.prompt, base_seed + 1, task.task_id)
        except InferenceError:
            raise
        except Exception as exc:
            raise InferenceError(str(exc), task.task_id) from exc

        inference_ms = int(time.monotonic() * 1000) - start_ms

        base = f"audio/{task.user_id}/{task.task_id}"
        key_a = f"{base}/candidate_a.mp3"
        key_b = f"{base}/candidate_b.mp3"

        # StorageError 向上传播，TaskWorker 会处理
        await self._storage.upload_audio(key_a, audio_a, task_id=task.task_id)
        await self._storage.upload_audio(key_b, audio_b, task_id=task.task_id)

        return InferenceResult(
            candidate_a_key=key_a,
            candidate_b_key=key_b,
            duration_seconds=30,  # TODO: 从 API 响应解析真实时长
            inference_ms=inference_ms,
        )

    async def _call_api(self, prompt: str, seed: int, task_id: int) -> bytes:
        """POST 到 SiliconFlow API，返回音频 bytes。
        兼容两种响应格式：
        - 直接返回 audio/mpeg binary
        - 返回 JSON，其中包含 CDN URL（再下载）
        """
        headers = {
            "Authorization": f"Bearer {self._api_key}",
            "Content-Type": "application/json",
        }
        payload = {
            "model": self._model,
            "prompt": prompt,
            "seed": seed,
        }

        async with httpx.AsyncClient(
            timeout=self._timeout,
            trust_env=False,
        ) as client:
            resp = await client.post(
                f"{self._base_url}{_GENERATE_PATH}",
                json=payload,
                headers=headers,
            )

        if resp.status_code == 401:
            raise InferenceError("SiliconFlow auth failed (401)", task_id)
        if resp.status_code == 429:
            raise InferenceError("SiliconFlow rate limited (429)", task_id)
        if resp.status_code != 200:
            raise InferenceError(
                f"SiliconFlow API error status={resp.status_code}", task_id
            )

        # Case 1: 直接返回 binary audio
        content_type = resp.headers.get("content-type", "")
        if "audio" in content_type or "octet-stream" in content_type:
            return resp.content

        # Case 2: 返回 JSON，包含 CDN URL
        body = resp.json()
        audio_url: str = (
            body.get("url")
            or body.get("audio_url")
            or body["data"][0]["url"]
        )
        async with httpx.AsyncClient(
            timeout=self._timeout,
            trust_env=False,
        ) as dl_client:
            dl_resp = await dl_client.get(audio_url)
            dl_resp.raise_for_status()
            return dl_resp.content
