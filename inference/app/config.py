"""配置管理（pydantic-settings，从环境变量或 .env 文件加载）"""

from functools import lru_cache

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        case_sensitive=False,
    )

    # Redis
    redis_url: str = "redis://localhost:6379/0"

    # Go API 回调
    go_api_base_url: str = "http://localhost:8080"
    internal_callback_secret: str = "dev_secret_change_in_production"

    # 推理后端：mock | musicgen | siliconflow
    inference_backend: str = "mock"

    # 七牛云 OSS
    qiniu_access_key: str = ""
    qiniu_secret_key: str = ""
    qiniu_bucket: str = "calliope-dev"
    qiniu_domain: str = ""
    qiniu_region: str = "z2"

    # Worker
    worker_id: str = "worker-local"
    stream_key: str = "calliope:tasks:stream"
    consumer_group: str = "inference-workers"
    stream_block_ms: int = 5000
    callback_max_retries: int = 3


@lru_cache
def get_settings() -> Settings:
    return Settings()
