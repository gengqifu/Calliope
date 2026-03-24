"""MusicGenInferenceService 单元测试（AudioCraft 模型被 mock，无需 GPU）"""

from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from app.exceptions import InferenceError, StorageError
from app.models.schemas import TaskMessage
from app.services.musicgen_service import MusicGenInferenceService


@pytest.fixture
def mock_storage() -> AsyncMock:
    storage = AsyncMock()
    storage.upload_audio.side_effect = lambda key, data, **kw: key
    return storage


@pytest.fixture
def svc(mock_storage: AsyncMock, mocker: MagicMock) -> MusicGenInferenceService:
    """已初始化的服务实例，_generate_one 被 mock（无需 torch）"""
    s = MusicGenInferenceService(
        storage=mock_storage,
        model_name="facebook/musicgen-small",
        duration=30,
    )
    s._model = MagicMock()  # 绕过 initialize()
    mocker.patch.object(s, "_generate_one", return_value=(b"fake_mp3", 30))
    return s


@pytest.fixture
def task() -> TaskMessage:
    return TaskMessage(
        task_id=42,
        user_id=1,
        prompt="upbeat electronic music",
        mode="instrumental",
        created_at="2026-03-23T10:00:00Z",
    )


@pytest.mark.asyncio
async def test_generate_returns_result(
    svc: MusicGenInferenceService, task: TaskMessage
) -> None:
    """成功生成时返回正确的 InferenceResult"""
    result = await svc.generate(task)

    assert result.candidate_a_key == "audio/1/42/candidate_a.mp3"
    assert result.candidate_b_key == "audio/1/42/candidate_b.mp3"
    assert result.duration_seconds == 30
    assert result.inference_ms >= 0


@pytest.mark.asyncio
async def test_generate_calls_generate_one_twice_with_different_seeds(
    svc: MusicGenInferenceService, task: TaskMessage
) -> None:
    """_generate_one 被调用两次，seed 分别为 task_id*1000 和 task_id*1000+1"""
    await svc.generate(task)

    calls = svc._generate_one.call_args_list  # type: ignore[attr-defined]
    assert len(calls) == 2
    seeds = [c.args[1] for c in calls]  # (prompt, seed)
    assert seeds[0] == task.task_id * 1000
    assert seeds[1] == task.task_id * 1000 + 1


@pytest.mark.asyncio
async def test_generate_raises_inference_error_on_model_failure(
    mock_storage: AsyncMock, task: TaskMessage, mocker: MagicMock
) -> None:
    """_generate_one 失败时包装为 InferenceError"""
    s = MusicGenInferenceService(storage=mock_storage)
    s._model = MagicMock()  # 绕过 initialize()
    mocker.patch.object(s, "_generate_one", side_effect=RuntimeError("CUDA out of memory"))

    with pytest.raises(InferenceError) as exc_info:
        await s.generate(task)

    assert exc_info.value.task_id == 42


@pytest.mark.asyncio
async def test_generate_not_initialized_raises_inference_error(
    mock_storage: AsyncMock, task: TaskMessage
) -> None:
    """未调用 initialize() 时 generate 应抛出 InferenceError"""
    s = MusicGenInferenceService(storage=mock_storage)
    # _model 仍为 None

    with pytest.raises(InferenceError):
        await s.generate(task)


@pytest.mark.asyncio
async def test_generate_storage_error_propagates(
    svc: MusicGenInferenceService, task: TaskMessage, mock_storage: AsyncMock
) -> None:
    """OSS 上传失败时 StorageError 向上传播"""
    mock_storage.upload_audio.side_effect = StorageError("oss error", task.task_id)

    with pytest.raises(StorageError):
        await svc.generate(task)


@pytest.mark.asyncio
async def test_generate_uploads_both_candidates(
    svc: MusicGenInferenceService, task: TaskMessage, mock_storage: AsyncMock
) -> None:
    """两个候选都被上传到 OSS"""
    await svc.generate(task)

    assert mock_storage.upload_audio.call_count == 2
    calls = [call.args[0] for call in mock_storage.upload_audio.call_args_list]
    assert "audio/1/42/candidate_a.mp3" in calls
    assert "audio/1/42/candidate_b.mp3" in calls


@pytest.mark.asyncio
async def test_initialize_loads_model(mock_storage: AsyncMock) -> None:
    """initialize() 应调用 MusicGen.get_pretrained 加载模型"""
    import sys

    s = MusicGenInferenceService(storage=mock_storage, model_name="facebook/musicgen-small")

    mock_instance = MagicMock()
    mock_musicgen_cls = MagicMock()
    mock_musicgen_cls.get_pretrained.return_value = mock_instance

    mock_audiocraft = MagicMock()
    mock_audiocraft_models = MagicMock()
    mock_audiocraft_models.MusicGen = mock_musicgen_cls

    with patch.dict(
        sys.modules,
        {
            "audiocraft": mock_audiocraft,
            "audiocraft.models": mock_audiocraft_models,
        },
    ):
        await s.initialize()

    mock_musicgen_cls.get_pretrained.assert_called_once_with("facebook/musicgen-small")
    assert s._model is mock_instance


@pytest.mark.slow
def test_tensor_to_mp3_bytes_returns_bytes(mock_storage: AsyncMock) -> None:
    """torchaudio MP3 编码（需要 ffmpeg 和 torch 安装）"""
    import torch

    s = MusicGenInferenceService(storage=mock_storage)
    wav = torch.zeros(1, 32000 * 5)  # 5 秒单声道
    result = s._tensor_to_mp3_bytes(wav, 32000)

    assert isinstance(result, bytes)
    assert len(result) > 0
