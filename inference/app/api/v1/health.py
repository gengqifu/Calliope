"""健康检查和队列统计接口"""

from fastapi import APIRouter, Request

from app.models.schemas import HealthResponse, QueueStatsResponse

router = APIRouter()


@router.get("/health", response_model=HealthResponse)
async def health(request: Request) -> HealthResponse:
    settings = request.app.state.settings
    return HealthResponse(
        status="ok",
        worker_id=settings.worker_id,
        inference_backend=settings.inference_backend,
    )


@router.get("/internal/queue/stats", response_model=QueueStatsResponse)
async def queue_stats(request: Request) -> QueueStatsResponse:
    worker = request.app.state.worker
    stats = await worker.get_stats()
    return QueueStatsResponse(**stats)
