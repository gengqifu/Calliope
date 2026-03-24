"""main.py 工厂函数单元测试"""

import pytest

from app.config import Settings
from app.main import _build_inference_service, _validate_config
from app.services.inference import MockInferenceService
from app.services.musicgen_service import MusicGenInferenceService
from app.services.siliconflow_service import SiliconFlowInferenceService


def test_build_inference_service_mock() -> None:
    """inference_backend=mock 时返回 MockInferenceService"""
    settings = Settings(inference_backend="mock")
    svc = _build_inference_service(settings)
    assert isinstance(svc, MockInferenceService)


def test_build_inference_service_musicgen() -> None:
    """inference_backend=musicgen 时返回 MusicGenInferenceService"""
    settings = Settings(
        inference_backend="musicgen",
        qiniu_access_key="ak",
        qiniu_secret_key="sk",
        qiniu_bucket="bucket",
    )
    svc = _build_inference_service(settings)
    assert isinstance(svc, MusicGenInferenceService)


def test_build_inference_service_siliconflow() -> None:
    """inference_backend=siliconflow 时返回 SiliconFlowInferenceService"""
    settings = Settings(
        inference_backend="siliconflow",
        qiniu_access_key="ak",
        qiniu_secret_key="sk",
        qiniu_bucket="bucket",
        siliconflow_api_key="test_key",
    )
    svc = _build_inference_service(settings)
    assert isinstance(svc, SiliconFlowInferenceService)


def test_build_inference_service_musicgen_uses_config() -> None:
    """MusicGen 服务使用 settings 中的模型名和时长"""
    settings = Settings(
        inference_backend="musicgen",
        qiniu_access_key="ak",
        qiniu_secret_key="sk",
        qiniu_bucket="bucket",
        musicgen_model_name="facebook/musicgen-medium",
        musicgen_default_duration=15,
    )
    svc = _build_inference_service(settings)
    assert isinstance(svc, MusicGenInferenceService)
    assert svc._model_name == "facebook/musicgen-medium"
    assert svc._duration == 15


def test_build_inference_service_siliconflow_uses_config() -> None:
    """SiliconFlow 服务使用 settings 中的 API key 和模型"""
    settings = Settings(
        inference_backend="siliconflow",
        qiniu_access_key="ak",
        qiniu_secret_key="sk",
        qiniu_bucket="bucket",
        siliconflow_api_key="my_key",
        siliconflow_model="custom-model",
        siliconflow_timeout_seconds=60,
    )
    svc = _build_inference_service(settings)
    assert isinstance(svc, SiliconFlowInferenceService)
    assert svc._api_key == "my_key"
    assert svc._model == "custom-model"
    assert svc._timeout == 60


# --- _validate_config 校验测试 ---

def test_validate_config_mock_always_passes() -> None:
    """mock 后端不校验任何字段"""
    _validate_config(Settings(inference_backend="mock"))  # 不应抛出


def test_validate_config_musicgen_missing_all_qiniu() -> None:
    """musicgen 后端缺少所有 Qiniu 配置时报错，错误信息列出所有缺失字段"""
    settings = Settings(
        inference_backend="musicgen",
        qiniu_access_key="",
        qiniu_secret_key="",
        qiniu_bucket="",
    )
    with pytest.raises(ValueError) as exc_info:
        _validate_config(settings)
    msg = str(exc_info.value)
    assert "QINIU_ACCESS_KEY" in msg
    assert "QINIU_SECRET_KEY" in msg
    assert "QINIU_BUCKET" in msg


def test_validate_config_musicgen_missing_one_qiniu_field() -> None:
    """musicgen 后端只缺 secret_key 时，错误信息只列出该字段"""
    settings = Settings(
        inference_backend="musicgen",
        qiniu_access_key="ak",
        qiniu_secret_key="",
        qiniu_bucket="bucket",
    )
    with pytest.raises(ValueError) as exc_info:
        _validate_config(settings)
    msg = str(exc_info.value)
    assert "QINIU_SECRET_KEY" in msg
    assert "QINIU_ACCESS_KEY" not in msg


def test_validate_config_musicgen_passes_with_full_qiniu() -> None:
    """musicgen 后端 Qiniu 配置齐全时通过校验"""
    settings = Settings(
        inference_backend="musicgen",
        qiniu_access_key="ak",
        qiniu_secret_key="sk",
        qiniu_bucket="bucket",
    )
    _validate_config(settings)  # 不应抛出


def test_validate_config_siliconflow_missing_api_key() -> None:
    """siliconflow 后端缺少 API key 时报错"""
    settings = Settings(
        inference_backend="siliconflow",
        qiniu_access_key="ak",
        qiniu_secret_key="sk",
        qiniu_bucket="bucket",
        siliconflow_api_key="",
    )
    with pytest.raises(ValueError) as exc_info:
        _validate_config(settings)
    assert "SILICONFLOW_API_KEY" in str(exc_info.value)


def test_validate_config_siliconflow_missing_qiniu_and_api_key() -> None:
    """siliconflow 后端同时缺少 Qiniu 和 API key 时，错误信息列出全部缺失字段"""
    settings = Settings(
        inference_backend="siliconflow",
        qiniu_access_key="",
        qiniu_secret_key="sk",
        qiniu_bucket="bucket",
        siliconflow_api_key="",
    )
    with pytest.raises(ValueError) as exc_info:
        _validate_config(settings)
    msg = str(exc_info.value)
    assert "QINIU_ACCESS_KEY" in msg
    assert "SILICONFLOW_API_KEY" in msg


def test_validate_config_siliconflow_passes_with_full_config() -> None:
    """siliconflow 后端配置齐全时通过校验"""
    settings = Settings(
        inference_backend="siliconflow",
        qiniu_access_key="ak",
        qiniu_secret_key="sk",
        qiniu_bucket="bucket",
        siliconflow_api_key="key",
    )
    _validate_config(settings)  # 不应抛出


def test_build_inference_service_raises_on_missing_config() -> None:
    """_build_inference_service 在配置不完整时直接抛出，不构造任何服务"""
    settings = Settings(inference_backend="musicgen")  # Qiniu 字段默认为空

    with pytest.raises(ValueError):
        _build_inference_service(settings)
