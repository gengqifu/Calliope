"""Redis 异步连接管理"""

import redis.asyncio as aioredis
from redis.asyncio import Redis


def create_redis_client(redis_url: str) -> Redis:  # type: ignore[type-arg]
    return aioredis.from_url(redis_url, decode_responses=True)
