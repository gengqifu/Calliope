"""自定义异常类"""


class CallbackError(Exception):
    """回调 Go API 失败（401 认证错误或重试耗尽）"""

    def __init__(self, message: str, task_id: int) -> None:
        super().__init__(message)
        self.task_id = task_id


class CallbackAuthError(CallbackError):
    """回调被 Go API 拒绝（401），需人工介入"""


class InferenceError(Exception):
    """推理引擎执行失败"""

    def __init__(self, message: str, task_id: int) -> None:
        super().__init__(message)
        self.task_id = task_id


class StorageError(Exception):
    """音频文件上传 OSS 失败"""

    def __init__(self, message: str, task_id: int) -> None:
        super().__init__(message)
        self.task_id = task_id
