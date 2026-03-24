"""StorageClient 单元测试（Qiniu SDK 被 mock）"""

from unittest.mock import MagicMock, patch

import pytest

from app.exceptions import StorageError
from app.infra.storage import StorageClient


@pytest.fixture
def client() -> StorageClient:
    return StorageClient(
        access_key="test_ak",
        secret_key="test_sk",
        bucket="test-bucket",
    )


@pytest.mark.asyncio
async def test_upload_audio_returns_key(client: StorageClient, mocker: MagicMock) -> None:
    """成功上传后返回 OSS key"""
    mocker.patch.object(client, "_sync_upload", return_value=None)

    result = await client.upload_audio("audio/1/1/candidate_a.mp3", b"fake_mp3")

    assert result == "audio/1/1/candidate_a.mp3"


@pytest.mark.asyncio
async def test_upload_audio_passes_all_args_to_sync_upload(
    client: StorageClient, mocker: MagicMock
) -> None:
    """key / data / content_type / task_id 都被正确传递给 _sync_upload"""
    mock_sync = mocker.patch.object(client, "_sync_upload", return_value=None)

    await client.upload_audio("key", b"data", content_type="audio/wav", task_id=77)

    mock_sync.assert_called_once_with("key", b"data", "audio/wav", 77)


@pytest.mark.asyncio
async def test_upload_audio_raises_storage_error_on_exception(
    client: StorageClient, mocker: MagicMock
) -> None:
    """_sync_upload 抛出异常时，upload_audio 应包装为 StorageError"""
    mocker.patch.object(
        client, "_sync_upload", side_effect=RuntimeError("network error")
    )

    with pytest.raises(StorageError) as exc_info:
        await client.upload_audio("key", b"data", task_id=42)

    assert exc_info.value.task_id == 42


@pytest.mark.asyncio
async def test_upload_audio_reraises_storage_error(
    client: StorageClient, mocker: MagicMock
) -> None:
    """_sync_upload 直接抛出 StorageError 时原样传播"""
    mocker.patch.object(
        client, "_sync_upload", side_effect=StorageError("upload failed", 99)
    )

    with pytest.raises(StorageError) as exc_info:
        await client.upload_audio("key", b"data", task_id=1)

    assert exc_info.value.task_id == 99


def test_sync_upload_raises_storage_error_on_bad_status(
    client: StorageClient, mocker: MagicMock
) -> None:
    """Qiniu put_data 返回非 200 时抛出 StorageError，且 task_id 被正确保留"""
    mock_info = MagicMock()
    mock_info.status_code = 400
    mock_info.error = "bad request"

    mocker.patch("app.infra.storage.qiniu.put_data", return_value=(None, mock_info))
    mocker.patch.object(client._auth, "upload_token", return_value="fake_token")

    with pytest.raises(StorageError) as exc_info:
        client._sync_upload("key", b"data", "audio/mpeg", task_id=42)

    assert exc_info.value.task_id == 42


@pytest.mark.asyncio
async def test_upload_audio_preserves_task_id_when_qiniu_returns_non_200(
    client: StorageClient, mocker: MagicMock
) -> None:
    """Qiniu 返回非 200 时，upload_audio 抛出的 StorageError 携带正确的 task_id（不是 0）"""
    mock_info = MagicMock()
    mock_info.status_code = 503
    mock_info.error = "service unavailable"

    mocker.patch("app.infra.storage.qiniu.put_data", return_value=(None, mock_info))
    mocker.patch.object(client._auth, "upload_token", return_value="fake_token")

    with pytest.raises(StorageError) as exc_info:
        await client.upload_audio("key", b"data", task_id=99)

    assert exc_info.value.task_id == 99


def test_sync_upload_succeeds_on_200(
    client: StorageClient, mocker: MagicMock
) -> None:
    """Qiniu put_data 返回 200 时正常完成"""
    mock_info = MagicMock()
    mock_info.status_code = 200
    mock_ret = {"key": "audio/1/1/candidate_a.mp3"}

    mocker.patch("app.infra.storage.qiniu.put_data", return_value=(mock_ret, mock_info))
    mocker.patch.object(client._auth, "upload_token", return_value="fake_token")

    # 不应抛出异常
    client._sync_upload("key", b"data", "audio/mpeg")
