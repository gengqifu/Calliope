"""AudioCraft MusicGen 本地推理服务"""

from __future__ import annotations

import asyncio
import io
import logging
import time
from concurrent.futures import ThreadPoolExecutor
from typing import TYPE_CHECKING, Optional

from app.exceptions import InferenceError
from app.infra.storage import StorageClient
from app.models.schemas import InferenceResult, TaskMessage

if TYPE_CHECKING:
    import torch
    from audiocraft.models import MusicGen

logger = logging.getLogger(__name__)

# 单线程：GPU 推理串行化，避免显存溢出
_INFERENCE_EXECUTOR = ThreadPoolExecutor(max_workers=1, thread_name_prefix="musicgen")


class MusicGenInferenceService:
    """调用本地 AudioCraft MusicGen 模型生成音乐，需要 GPU 环境"""

    def __init__(
        self,
        storage: StorageClient,
        model_name: str = "facebook/musicgen-small",
        duration: int = 30,
    ) -> None:
        self._storage = storage
        self._model_name = model_name
        self._duration = duration
        self._model: Optional[MusicGen] = None

    async def initialize(self) -> None:
        """在线程池中加载模型权重，应在服务启动时调用一次"""
        loop = asyncio.get_event_loop()
        await loop.run_in_executor(_INFERENCE_EXECUTOR, self._load_model)
        logger.info("MusicGen model %s loaded", self._model_name)

    def _load_model(self) -> None:
        from audiocraft.models import MusicGen  # 延迟导入：避免模块加载时强依赖 torch

        self._model = MusicGen.get_pretrained(self._model_name)
        self._model.set_generation_params(duration=self._duration)

    async def generate(self, task: TaskMessage) -> InferenceResult:
        """生成两个候选音频，上传到 OSS，返回推理结果"""
        if self._model is None:
            raise InferenceError("Model not initialized, call initialize() first", task.task_id)

        start_ms = int(time.monotonic() * 1000)
        base_seed = task.task_id * 1000
        loop = asyncio.get_event_loop()

        try:
            mp3_a, duration = await loop.run_in_executor(
                _INFERENCE_EXECUTOR,
                self._generate_one,
                task.prompt,
                base_seed,
            )
            mp3_b, _ = await loop.run_in_executor(
                _INFERENCE_EXECUTOR,
                self._generate_one,
                task.prompt,
                base_seed + 1,
            )
        except InferenceError:
            raise
        except Exception as exc:
            raise InferenceError(str(exc), task.task_id) from exc

        inference_ms = int(time.monotonic() * 1000) - start_ms

        base = f"audio/{task.user_id}/{task.task_id}"
        key_a = f"{base}/candidate_a.mp3"
        key_b = f"{base}/candidate_b.mp3"

        # StorageError 向上传播，TaskWorker 会处理
        await self._storage.upload_audio(key_a, mp3_a, task_id=task.task_id)
        await self._storage.upload_audio(key_b, mp3_b, task_id=task.task_id)

        return InferenceResult(
            candidate_a_key=key_a,
            candidate_b_key=key_b,
            duration_seconds=duration,
            inference_ms=inference_ms,
        )

    def _generate_one(self, prompt: str, seed: int) -> tuple[bytes, int]:
        """同步推理，在线程池中执行。返回 (mp3_bytes, duration_seconds)"""
        import torch  # 延迟导入

        torch.manual_seed(seed)
        # generate() 返回 tensor: [batch=1, channels, samples]
        wav = self._model.generate([prompt])  # type: ignore[union-attr]
        wav = wav[0]  # [channels, samples]
        sr = self._model.sample_rate  # type: ignore[union-attr]
        duration_seconds = int(wav.shape[-1] / sr)
        mp3_bytes = self._tensor_to_mp3_bytes(wav, sr)
        return mp3_bytes, duration_seconds

    def _tensor_to_mp3_bytes(self, wav: torch.Tensor, sample_rate: int) -> bytes:
        """将 AudioCraft 输出张量编码为 MP3 bytes（依赖 ffmpeg）"""
        import torchaudio  # 延迟导入

        buf = io.BytesIO()
        wav_cpu = wav.cpu().float()
        # torchaudio.save 支持写入 BytesIO，format="mp3" 需要 ffmpeg backend
        torchaudio.save(buf, wav_cpu, sample_rate, format="mp3")
        buf.seek(0)
        return buf.read()
