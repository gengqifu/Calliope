"""Pydantic 请求/响应模型"""

from pydantic import BaseModel, Field


class TaskMessage(BaseModel):
    """从 Redis Stream 读取的任务消息"""

    task_id: int
    user_id: int
    prompt: str = Field(..., min_length=1, max_length=200)
    mode: str = Field(..., pattern="^(vocal|instrumental)$")
    created_at: str  # RFC3339 UTC


class InferenceResult(BaseModel):
    """推理完成后的结果"""

    candidate_a_key: str  # OSS 存储路径
    candidate_b_key: str
    duration_seconds: int
    inference_ms: int


class HealthResponse(BaseModel):
    status: str
    worker_id: str
    inference_backend: str


class QueueStatsResponse(BaseModel):
    stream_length: int
    pending_count: int
    current_task_id: int | None = None
