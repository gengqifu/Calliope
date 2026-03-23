"""OSS 存储客户端骨架（当前为 Mock，真实实现在 AudioCraft 集成阶段引入）"""

import logging

logger = logging.getLogger(__name__)


class StorageClient:
    """骨架：Mock 实现直接返回假路径，不做真实上传"""

    def __init__(self, bucket: str) -> None:
        self._bucket = bucket

    async def upload_audio(
        self, key: str, data: bytes, content_type: str = "audio/mpeg"
    ) -> str:
        """上传音频文件，返回 OSS key。骨架阶段直接返回 key 本身。"""
        logger.debug("mock upload key=%s size=%d", key, len(data))
        return key
