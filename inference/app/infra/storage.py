"""七牛云 OSS 存储客户端"""

import asyncio
import logging
from concurrent.futures import ThreadPoolExecutor

import qiniu

from app.exceptions import StorageError

logger = logging.getLogger(__name__)

_STORAGE_EXECUTOR = ThreadPoolExecutor(max_workers=4, thread_name_prefix="storage")


class StorageClient:
    """七牛云 OSS 上传客户端，同步 SDK 方法通过线程池异步化"""

    def __init__(self, access_key: str, secret_key: str, bucket: str) -> None:
        self._bucket = bucket
        self._auth = qiniu.Auth(access_key, secret_key)

    async def upload_audio(
        self,
        key: str,
        data: bytes,
        content_type: str = "audio/mpeg",
        task_id: int = 0,
    ) -> str:
        """上传音频到 Qiniu OSS，返回 OSS key"""
        loop = asyncio.get_event_loop()
        try:
            await loop.run_in_executor(
                _STORAGE_EXECUTOR,
                self._sync_upload,
                key,
                data,
                content_type,
                task_id,
            )
        except StorageError:
            raise
        except Exception as exc:
            raise StorageError(str(exc), task_id) from exc
        return key

    def _sync_upload(
        self, key: str, data: bytes, content_type: str, task_id: int = 0
    ) -> None:
        """阻塞式 Qiniu 上传，在线程池中执行"""
        token = self._auth.upload_token(self._bucket, key)
        _ret, info = qiniu.put_data(token, key, data, mime_type=content_type)
        if info.status_code != 200:
            raise StorageError(
                f"Qiniu upload failed: status={info.status_code} error={info.error}",
                task_id=task_id,
            )
