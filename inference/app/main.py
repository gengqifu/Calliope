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
from app.services.inference import MockInferenceService
from app.worker.task_worker import TaskWorker

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(name)s %(message)s",
)


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncGenerator[None, None]:
    settings: Settings = app.state.settings

    redis = create_redis_client(settings.redis_url)
    callback = CallbackClient(
        settings.go_api_base_url,
        settings.internal_callback_secret,
        max_retries=settings.callback_max_retries,
    )
    inference = MockInferenceService()

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
