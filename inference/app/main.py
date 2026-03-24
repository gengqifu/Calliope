"""FastAPI 应用入口"""

import asyncio
import logging
from contextlib import asynccontextmanager
from typing import AsyncGenerator

from fastapi import FastAPI

from app.api.v1.health import router as health_router
from app.config import Settings, get_settings
from app.infra.callback_client import CallbackClient
from app.infra.redis_client import create_redis_client
from app.infra.storage import StorageClient
from app.services.inference import InferenceService, MockInferenceService
from app.services.musicgen_service import MusicGenInferenceService
from app.services.siliconflow_service import SiliconFlowInferenceService
from app.worker.task_worker import TaskWorker

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(name)s %(message)s",
)


def _validate_config(settings: Settings) -> None:
    """非 mock 后端的启动期配置校验，缺失必填项直接 raise，不等到任务处理时才报错"""
    backend = settings.inference_backend
    if backend == "mock":
        return

    missing: list[str] = []
    if not settings.qiniu_access_key:
        missing.append("QINIU_ACCESS_KEY")
    if not settings.qiniu_secret_key:
        missing.append("QINIU_SECRET_KEY")
    if not settings.qiniu_bucket:
        missing.append("QINIU_BUCKET")

    if backend == "siliconflow" and not settings.siliconflow_api_key:
        missing.append("SILICONFLOW_API_KEY")

    if missing:
        raise ValueError(
            f"inference_backend={backend!r} requires these env vars to be set: "
            + ", ".join(missing)
        )


def _build_inference_service(settings: Settings) -> InferenceService:
    """根据配置构建推理服务实例，构建前先做 fail-fast 配置校验"""
    _validate_config(settings)

    if settings.inference_backend == "mock":
        return MockInferenceService()

    storage = StorageClient(
        access_key=settings.qiniu_access_key,
        secret_key=settings.qiniu_secret_key,
        bucket=settings.qiniu_bucket,
    )

    if settings.inference_backend == "musicgen":
        return MusicGenInferenceService(
            storage=storage,
            model_name=settings.musicgen_model_name,
            duration=settings.musicgen_default_duration,
        )

    if settings.inference_backend == "siliconflow":
        return SiliconFlowInferenceService(
            storage=storage,
            api_key=settings.siliconflow_api_key,
            base_url=settings.siliconflow_base_url,
            model=settings.siliconflow_model,
            timeout_seconds=settings.siliconflow_timeout_seconds,
        )

    raise ValueError(f"Unknown inference_backend: {settings.inference_backend}")


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncGenerator[None, None]:
    settings: Settings = app.state.settings

    redis = create_redis_client(settings.redis_url)
    callback = CallbackClient(
        settings.go_api_base_url,
        settings.internal_callback_secret,
        max_retries=settings.callback_max_retries,
    )
    inference = _build_inference_service(settings)

    # MusicGen 需要在启动时加载模型权重
    if hasattr(inference, "initialize"):
        await inference.initialize()

    worker = TaskWorker(
        redis=redis,
        callback=callback,
        inference=inference,
        stream_key=settings.stream_key,
        consumer_group=settings.consumer_group,
        worker_id=settings.worker_id,
        block_ms=settings.stream_block_ms,
    )
    app.state.worker = worker

    bg_task = asyncio.create_task(worker.run())
    try:
        yield
    finally:
        bg_task.cancel()
        try:
            await bg_task
        except asyncio.CancelledError:
            pass
        await redis.aclose()


def create_app(settings: Settings | None = None) -> FastAPI:
    if settings is None:
        settings = get_settings()

    app = FastAPI(title="Calliope Inference Service", version="0.1.0", lifespan=lifespan)
    app.state.settings = settings

    app.include_router(health_router)

    return app


app = create_app()
