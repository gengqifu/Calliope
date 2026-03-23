"""推理服务接口与 Mock 实现

Protocol 定义接口，骨架阶段用 MockInferenceService 替代真实推理。
AudioCraft 集成时只需替换实现，Worker 代码无需修改。
"""

import asyncio
import time
from typing import Protocol

from app.models.schemas import InferenceResult, TaskMessage


class InferenceService(Protocol):
    async def generate(self, task: TaskMessage) -> InferenceResult:
        """执行推理，返回两个候选音频的 OSS key 及元信息"""
        ...


class MockInferenceService:
    """Mock 推理：sleep 2 秒后返回固定 OSS key，用于骨架联调"""

    async def generate(self, task: TaskMessage) -> InferenceResult:
        start_ms = int(time.monotonic() * 1000)
        # 模拟 GPU 推理耗时（run_in_executor 占位符，真实推理需放线程池）
        await asyncio.sleep(2)
        inference_ms = int(time.monotonic() * 1000) - start_ms

        base = f"audio/{task.user_id}/{task.task_id}"
        return InferenceResult(
            candidate_a_key=f"{base}/candidate_a.mp3",
            candidate_b_key=f"{base}/candidate_b.mp3",
            duration_seconds=30,
            inference_ms=inference_ms,
        )
