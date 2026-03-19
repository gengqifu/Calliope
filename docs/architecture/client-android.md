# Android 客户端架构设计

> 版本：v1.0
> 更新日期：2026-03-17
> 适用平台：Android（Kotlin，minSdk 26）

---

## 1. 概述

Calliope Android 客户端采用 **MVVM + Clean Architecture 简化版**，以 Jetpack 组件为核心，Hilt 做依赖注入，Coroutines + StateFlow 驱动异步与状态管理。本文档描述代码组织方式、依赖选型、关键实现机制，以及与 `client-sdk-spec.md` 中各 SDK 接口的类对应关系。

---

## 2. 架构总览

```
┌─────────────────────────────────────────────────────────┐
│                     UI Layer                             │
│   Compose Screen   ←→   ViewModel（hiltViewModel()）    │
│   (collectAsStateWithLifecycle，触发 Intent / Event)     │
└──────────────────────┬──────────────────────────────────┘
                       │ 调用 UseCase（可选）/ Repository
┌──────────────────────▼──────────────────────────────────┐
│                   Domain Layer（可选）                    │
│   UseCase：单一业务操作，封装 Repository 调用组合          │
└──────────────────────┬──────────────────────────────────┘
                       │ 调用
┌──────────────────────▼──────────────────────────────────┐
│                    Data Layer                            │
│   Repository：聚合 Remote + Local，对上层屏蔽数据来源      │
│   RemoteDataSource：Retrofit API 调用                    │
│   LocalDataSource：TokenStorage（加密本地存储）            │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│                  Network / Platform                      │
│   OkHttpClient（Authenticator + Interceptor）            │
│   WsTransport（OkHttp WebSocket，per-task 连接）            │
│   ExoPlayer（Media3 音频播放）                             │
└─────────────────────────────────────────────────────────┘
```

---

## 3. 分层职责

| 层 | 类/包 | 职责 | 禁止 |
|---|---|---|---|
| UI Layer | `ui/*Screen`, `ui/*ViewModel` | 展示状态、处理用户事件、生命周期管理 | 直接访问网络或数据库 |
| Domain Layer | `domain/usecase/` | 组合多个 Repository 操作，封装业务规则 | 依赖 Android 框架类 |
| Data Layer | `data/repository/` | 决定数据来源（远程/本地）、缓存策略 | 包含 UI 逻辑 |
| Remote | `data/remote/api/`, `data/remote/dto/` | Retrofit 接口定义、DTO 序列化 | 业务判断 |
| Local | `data/local/TokenStorage` | EncryptedSharedPreferences 读写 | 网络调用 |
| Network | `network/` | HTTP 客户端装配、Token 自动刷新、WebSocket 管理 | 业务逻辑 |

---

## 4. 依赖选型

| 用途 | 库 | 版本 | 说明 |
|---|---|---|---|
| 依赖注入 | Hilt | 2.51 | Jetpack 官方推荐，与 ViewModel 深度集成 |
| HTTP 客户端 | OkHttp | 4.12.0 | Token Authenticator / Interceptor 生态完善 |
| REST 封装 | Retrofit | 2.11.0 | 接口描述式，与 OkHttp 同栈 |
| JSON 序列化 | kotlinx.serialization | 1.6.3 | Kotlin 原生，编译期安全 |
| 异步 | Kotlin Coroutines | 1.8.0 | Suspend + Flow 统一异步模型 |
| 状态管理 | StateFlow / SharedFlow | — | 随 Coroutines，替代 LiveData |
| WebSocket | OkHttp WebSocket | 随 OkHttp | 与 HTTP 客户端同实例，复用连接池 |
| 音频播放 | ExoPlayer (Media3) | 1.3.1 | 支持流式播放、进度回调、后台播放 |
| 安全存储 | EncryptedSharedPreferences | AndroidX Security 1.1.0 | AT/RT 加密存储 |
| UI 框架 | Jetpack Compose BOM | 2024.05.00 | 声明式 UI，与 ViewModel / StateFlow 原生集成 |
| Compose 导航 | Navigation Compose | 2.7.7 | `NavHost` + `composable` 路由，替代 Fragment 导航 |
| 图片加载 | Coil（compose） | 2.6.0 | `AsyncImage` Composable，协程原生 |

---

## 5. 目录结构

```
app/src/main/java/com/calliope/
│
├── ui/
│   ├── nav/
│   │   └── AppNavGraph.kt            # NavHost 定义，所有路由集中管理
│   ├── auth/
│   │   ├── LoginScreen.kt            # @Composable fun LoginScreen(vm: AuthViewModel)
│   │   ├── RegisterScreen.kt
│   │   └── AuthViewModel.kt          # 对应 AuthSDK：login / register / logout
│   ├── task/
│   │   ├── CreateTaskScreen.kt
│   │   ├── TaskProgressScreen.kt
│   │   └── TaskViewModel.kt          # 对应 TaskSDK：createTask / watchTask
│   ├── work/
│   │   ├── WorkListScreen.kt
│   │   ├── WorkDetailScreen.kt
│   │   ├── WorkViewModel.kt          # 对应 WorkSDK：listWorks / selectCandidate
│   │   └── PlayerViewModel.kt        # 对应 AudioPlayer：play / pause / seek
│   └── common/
│       ├── LoadingOverlay.kt         # @Composable，全屏 Loading
│       ├── ErrorSnackbar.kt          # @Composable，错误提示
│       └── ErrorHandler.kt           # API 错误码 → UiError 映射（纯 Kotlin，无 UI 依赖）
│
├── domain/
│   └── usecase/                       # MVP 阶段可选；业务逻辑简单时直接在 ViewModel 调用 Repository
│       ├── CreateTaskUseCase.kt
│       └── SelectCandidateUseCase.kt
│
├── data/
│   ├── repository/
│   │   ├── AuthRepository.kt
│   │   ├── TaskRepository.kt
│   │   ├── WorkRepository.kt
│   │   └── CreditRepository.kt
│   ├── remote/
│   │   ├── api/
│   │   │   ├── AuthApi.kt             # Retrofit interface
│   │   │   ├── TaskApi.kt
│   │   │   ├── WorkApi.kt
│   │   │   └── CreditApi.kt
│   │   └── dto/
│   │       ├── AuthDto.kt
│   │       ├── TaskDto.kt
│   │       ├── WorkDto.kt
│   │       └── ErrorDto.kt
│   └── local/
│       └── TokenStorage.kt            # EncryptedSharedPreferences 封装（AT/RT/过期时间）
│
├── network/
│   ├── OkHttpClientFactory.kt         # 装配 Interceptor + Authenticator，注入到 Retrofit
│   ├── TokenAuthenticator.kt          # 401 自动刷新 RT → 重发原请求
│   ├── AuthInterceptor.kt             # 所有请求注入 Authorization: Bearer {AT}
│   └── WsTransport.kt                 # per-task WS callbackFlow 封装（无全局长连接）
│
├── audio/
│   └── AudioPlayerManager.kt          # ExoPlayer 封装，暴露 StateFlow<PlayerState>
│
└── di/
    ├── NetworkModule.kt               # @Provides OkHttpClient, Retrofit, API interfaces
    ├── RepositoryModule.kt            # @Binds Repository 实现
    └── StorageModule.kt               # @Provides TokenStorage
```

---

## 6. 关键实现机制

### 6.1 Token 自动刷新

```kotlin
// network/TokenAuthenticator.kt
class TokenAuthenticator @Inject constructor(
    private val tokenStorage: TokenStorage,
    private val authApi: AuthApi,          // 独立 Retrofit 实例，不含 Authenticator，防递归
) : Authenticator {

    override fun authenticate(route: Route?, response: Response): Request? {
        // 防止并发多次刷新（同一时刻多个请求 401）
        synchronized(this) {
            val currentToken = tokenStorage.getAccessToken()
            // 如果 token 已被其他线程刷新，直接用新 token 重试
            if (currentToken != null && currentToken != response.request.header("Authorization")
                    ?.removePrefix("Bearer ")) {
                return response.request.newBuilder()
                    .header("Authorization", "Bearer $currentToken")
                    .build()
            }

            val refreshToken = tokenStorage.getRefreshToken() ?: return null  // 未登录，放弃

            return try {
                val result = runBlocking { authApi.refresh(RefreshRequest(refreshToken)) }
                // 注：TokenPair 只含 accessToken + expiresAt，后端不轮换 RT（client-sdk-spec.md §3.1）
                tokenStorage.saveAccessToken(result.accessToken, result.expiresAt)
                response.request.newBuilder()
                    .header("Authorization", "Bearer ${result.accessToken}")
                    .build()
            } catch (e: Exception) {
                tokenStorage.clear()
                // 发出登出事件，UI 层监听后跳转登录页
                // authenticate() 是同步函数（OkHttp Authenticator 接口），不能调用挂起版 emit()
                // AuthEventBus 内部用 tryEmit() 封装，保证在非挂起上下文可直接调用
                AuthEventBus.post(AuthEvent.SessionExpired)
                null
            }
        }
    }
}
```

**要点：**
- `AuthApi` 必须使用**独立的 OkHttpClient**（不含 `TokenAuthenticator`），防止刷新接口本身 401 时无限递归
- `synchronized` 块防止并发刷新；已刷新的请求直接用新 token 重试，不再调用 refresh
- 刷新失败时清空 token，通过 `AuthEventBus` 通知所有 ViewModel 跳登录页；`AuthEventBus` 设计为 singleton，内部持有 `MutableSharedFlow<AuthEvent>(extraBufferCapacity = 1)`，对外暴露同步方法 `fun post(event: AuthEvent) { _events.tryEmit(event) }`，使 `authenticate()`（同步函数）可直接调用而无需挂起上下文

**主动刷新（规范 §3.3 第一层：请求前 60s 主动刷新）** 在 `AuthInterceptor` 中实现：

```kotlin
// network/AuthInterceptor.kt
// 规范要求两层保障：① 请求前主动刷新（AuthInterceptor）② 401 兜底刷新（TokenAuthenticator）
class AuthInterceptor @Inject constructor(
    private val tokenStorage: TokenStorage,
    private val authApi: AuthApi,   // 独立 OkHttpClient，不含 Authenticator/Interceptor
) : Interceptor {

    override fun intercept(chain: Interceptor.Chain): Response {
        // 若 AT 在 60s 内到期，提前刷新，避免请求途中过期（对 WS 握手尤为重要）
        if (tokenStorage.isExpiringSoon()) {
            synchronized(this) {
                if (tokenStorage.isExpiringSoon()) {  // double-check
                    val rt = tokenStorage.getRefreshToken()
                    if (rt != null) {
                        runCatching {
                            val result = runBlocking { authApi.refresh(RefreshRequest(rt)) }
                            tokenStorage.saveAccessToken(result.accessToken, result.expiresAt)
                        }
                        // 刷新失败不阻断请求，让 TokenAuthenticator 在 401 时兜底
                    }
                }
            }
        }

        val token = tokenStorage.getAccessToken()
        val request = if (token != null) {
            chain.request().newBuilder()
                .header("Authorization", "Bearer $token")
                .build()
        } else chain.request()

        return chain.proceed(request)
    }
}
```

> `AuthInterceptor` 处理**主动刷新**，`TokenAuthenticator` 处理 **401 兜底**，两者协作覆盖规范 §3.3 的完整策略。`TokenStorage.isExpiringSoon()` 实现：`expiresAt - 60s <= now()`。

---

### 6.2 WebSocket 传输层（per-task 连接）

SDK 规范（§4.3）要求 WS URL 带 `task_id` 参数：`wss://...?token=...&task_id=...`，这意味着每次 `watchTask` 建立独立的 per-task 连接，**不存在共享的全局 WS 连接**。`WsTransport` 封装单条 per-task 连接的生命周期，由 `TaskRepository.watchTask` 按需创建：

```kotlin
// network/WsTransport.kt
// 每次 watchTask 实例化一次，不是单例；由 TaskRepository 在调用方 scope 内管理生命周期

class WsTransport(private val okHttpClient: OkHttpClient) {

    // 返回 Flow<TaskStatus>，collect 期间持有连接；Flow 取消时自动关闭 WS
    fun openTaskChannel(taskId: Long, token: String): Flow<TaskStatus> = callbackFlow {
        val url = "wss://api.calliope-music.com/ws?token=$token&task_id=$taskId"
        val request = Request.Builder().url(url).build()

        val ws = okHttpClient.newWebSocket(request, object : WebSocketListener() {
            override fun onMessage(ws: WebSocket, text: String) {
                val status = parseTaskStatus(text)
                trySend(status)  // 不会 block；callbackFlow buffer 默认 64
            }
            override fun onFailure(ws: WebSocket, t: Throwable, response: Response?) {
                close(t)  // 触发 Flow 异常，tryWatchViaWs catch 后降级轮询
            }
            override fun onClosed(ws: WebSocket, code: Int, reason: String) {
                close()   // 服务端关闭（终态后服务端断开），Flow 正常结束
            }
        })

        // callbackFlow 的清理回调：Flow 被取消（cancelWatch）时关闭 WS
        awaitClose { ws.close(1000, "Watch cancelled") }
    }
}
```

**`WsTransport` 的依赖注入：** 不应标注 `@Singleton`，每次 `watchTask` 由 `TaskRepository` 用 `OkHttpClient`（单例）新建实例，或通过工厂注入：

```kotlin
// di/NetworkModule.kt
@Provides
fun provideWsTransportFactory(okHttpClient: OkHttpClient) =
    WsTransportFactory { WsTransport(okHttpClient) }

fun interface WsTransportFactory { fun create(): WsTransport }
```

> **与 HTTP OkHttpClient 的关系：** `WsTransport` 复用同一个 `OkHttpClient` 实例（共享连接池和线程池），但每个 WS 连接是独立的 TCP 连接，不是 sub-channel。

---

### 6.3 watchTask 实现（WS + 轮询降级 + 210s 超时 + CancelHandle）

`WsTransport`（6.2）为每次 `watchTask` 调用建立一条独立的 per-task WS 连接，不存在全局长连接。`watchTask` 对应 SDK 规范中的独立监听契约，在 `TaskRepository` 中实现：

```kotlin
// data/repository/TaskRepository.kt

// 对应 SDK 规范的 CancelHandle
fun interface CancelHandle { fun cancel() }

fun watchTask(
    taskId: Long,
    onUpdate: (TaskStatus) -> Unit,
    onError: (Throwable) -> Unit,
    scope: CoroutineScope,
): CancelHandle {
    val job = scope.launch {
        try {
            withTimeout(210_000L) {   // 客户端 210s 超时（规范 §4.3）
                val wsConsumed = tryWatchViaWs(taskId, onUpdate)
                if (!wsConsumed) {
                    // WS 连接失败，降级为轮询（间隔 3s，规范 §4.3）
                    pollUntilTerminal(taskId, onUpdate)
                }
            }
        } catch (e: TimeoutCancellationException) {
            onError(TaskTimeoutException("任务等待超时（210s）"))
        } catch (e: CancellationException) {
            // cancelWatch 正常取消，不回调 onError
        } catch (e: Throwable) {
            onError(e)
        }
    }
    return CancelHandle { job.cancel() }
}

// WS 监听：每个 taskId 建立独立的 per-task WS 连接（见下方 6.2 说明）
// 收到 completed/failed 后返回 true（已消费完毕）；连接失败返回 false（触发降级）
private suspend fun tryWatchViaWs(taskId: Long, onUpdate: (TaskStatus) -> Unit): Boolean {
    return try {
        val token = tokenStorage.getAccessToken() ?: return false
        wsTransport.openTaskChannel(taskId, token)
            .onEach { status -> onUpdate(status) }           // 先回调（含终态）
            .takeWhile { status ->                           // 再判断是否继续
                status.status != "completed" && status.status != "failed"
            }
            // takeWhile 在终态时终止 Flow；服务端不主动断开时也能正确退出
            .collect()
        true
    } catch (e: CancellationException) {
        throw e  // 主动取消（cancelWatch）必须放行，不能降级为轮询
    } catch (e: Exception) {
        false    // 真正的 WS 连接异常，降级轮询
    }
}

private suspend fun pollUntilTerminal(taskId: Long, onUpdate: (TaskStatus) -> Unit) {
    var lastStatus: String? = null
    while (true) {
        val status = taskApi.getTask(taskId)
        if (status.status != lastStatus) {
            onUpdate(status)
            lastStatus = status.status
        }
        if (status.status == "completed" || status.status == "failed") break
        delay(3_000L)
    }
}
```

**`cancelWatch` 映射：**
```kotlin
// TaskViewModel.kt
private var watchHandle: CancelHandle? = null

fun startWatch(taskId: Long) {
    watchHandle = taskRepository.watchTask(
        taskId = taskId,
        onUpdate = { status -> _taskState.value = status.toUiState() },
        onError  = { e -> _error.value = e.toUiError() },
        scope    = viewModelScope,
    )
}

override fun onCleared() {
    watchHandle?.cancel()   // cancelWatch(handle)，不影响其他任务的连接
}
```

> **与 6.2 的关系：** `WsTransport.openTaskChannel` 每次创建独立的 per-task WS 连接；`cancelWatch` 取消 coroutine job，`callbackFlow` 的 `awaitClose` 回调负责关闭该连接，不影响其他正在进行的 watchTask。

---

### 6.4 音频播放状态机

```kotlin
// audio/AudioPlayerManager.kt

// 对齐 SDK 规范 §7.1：idle | loading | ready | playing | paused | ended | error
enum class PlayerState { IDLE, LOADING, READY, PLAYING, PAUSED, ENDED, ERROR }

data class ProgressState(val currentSeconds: Int, val totalSeconds: Int)

// AudioPlayerManager 不是 ViewModel，不能使用 viewModelScope。
// 网络错误的重试逻辑（需要 delay + 异步）上移到 PlayerViewModel，
// AudioPlayerManager 只负责将错误暴露出来。
class AudioPlayerManager @Inject constructor(
    @ApplicationContext private val context: Context,
) {
    private val player = ExoPlayer.Builder(context).build()
    private val _state = MutableStateFlow(PlayerState.IDLE)
    val state: StateFlow<PlayerState> = _state.asStateFlow()

    // 进度不在 manager 内维护：ExoPlayer 无内置进度 Flow，进度由 PlayerViewModel
    // 通过 currentPositionSeconds() / totalDurationSeconds() 轮询后写入自己的 StateFlow
    var currentUrl: String? = null
        private set

    // 对齐规范 §7.3：仅网络错误触发重试；PlayerViewModel 通过此字段区分错误类型
    var isLastErrorNetwork: Boolean = false
        private set

    init {
        player.addListener(object : Player.Listener {
            override fun onPlaybackStateChanged(playbackState: Int) {
                _state.value = when (playbackState) {
                    Player.STATE_BUFFERING -> PlayerState.LOADING
                    Player.STATE_READY     -> if (player.isPlaying) PlayerState.PLAYING else PlayerState.READY
                    Player.STATE_ENDED     -> PlayerState.ENDED
                    else                   -> PlayerState.IDLE
                }
            }
            override fun onIsPlayingChanged(isPlaying: Boolean) {
                if (player.playbackState == Player.STATE_READY) {
                    _state.value = if (isPlaying) PlayerState.PLAYING else PlayerState.PAUSED
                }
            }
            override fun onPlayerError(error: PlaybackException) {
                // 仅纯网络连接/超时错误才重试（规范 §7.3）
                // BAD_HTTP_STATUS 包含 404/403 等服务端错误，重试无意义，不纳入
                isLastErrorNetwork = error.errorCode in setOf(
                    PlaybackException.ERROR_CODE_IO_NETWORK_CONNECTION_FAILED,
                    PlaybackException.ERROR_CODE_IO_NETWORK_CONNECTION_TIMEOUT,
                )
                _state.value = PlayerState.ERROR
            }
        })
    }

    fun load(url: String) {
        currentUrl = url
        _state.value = PlayerState.LOADING
        player.setMediaItem(MediaItem.fromUri(url))
        player.prepare()
    }

    fun play()  { player.play() }
    fun pause() { player.pause() }

    // 对齐规范 §7.1 seek(positionSeconds)
    fun seek(positionSeconds: Int) {
        val duration = player.duration
        if (duration != C.TIME_UNSET && duration > 0) {
            player.seekTo(positionSeconds * 1000L)
        }
    }

    // 对齐规范 §7.1 setVolume(0.0 ~ 1.0)
    fun setVolume(volume: Float) {
        player.volume = volume.coerceIn(0f, 1f)
    }

    // 供 PlayerViewModel 进度轮询使用（ExoPlayer 无内置进度 Flow）
    fun currentPositionSeconds(): Int = (player.currentPosition / 1000).toInt()
    fun totalDurationSeconds(): Int {
        val total = player.duration
        return if (total != C.TIME_UNSET && total > 0) (total / 1000).toInt() else 0
    }

    fun release() {
        player.release()
        currentUrl = null
    }
}
```

**网络错误自动恢复（规范 §7.3）在 `PlayerViewModel` 中实现：**

```kotlin
// ui/work/PlayerViewModel.kt
private var retryCount = 0

init {
    // 错误恢复：仅网络错误重试，非网络错误（404/解码失败等）直接报错
    viewModelScope.launch {
        audioPlayerManager.state.collect { state ->
            if (state == PlayerState.ERROR) {
                val url = audioPlayerManager.currentUrl
                if (url != null && retryCount < 3 && audioPlayerManager.isLastErrorNetwork) {
                    retryCount++
                    val savedPositionSecs = audioPlayerManager.currentPositionSeconds()
                    delay(2_000L)
                    audioPlayerManager.load(url)
                    // 等待播放器就绪（规范 §7.3：load → 就绪后 seek → play）
                    viewModelScope.launch {
                        audioPlayerManager.state
                            .filter { it == PlayerState.READY }
                            .first()
                        audioPlayerManager.seek(savedPositionSecs)
                        audioPlayerManager.play()
                    }
                }
                // 非网络错误或超过 3 次：state 保持 ERROR，UI 层显示错误
            } else if (state == PlayerState.PLAYING) {
                retryCount = 0  // 恢复成功，重置计数
            }
        }
    }

    // 进度轮询：每 500ms 采样一次，仅在播放中更新（规范 §7.1 onProgress）
    viewModelScope.launch {
        while (true) {
            if (audioPlayerManager.state.value == PlayerState.PLAYING) {
                _progress.value = ProgressState(
                    currentSeconds = audioPlayerManager.currentPositionSeconds(),
                    totalSeconds   = audioPlayerManager.totalDurationSeconds(),
                )
            }
            delay(500L)
        }
    }
}
```

---

### 6.5 audioUrl 到期刷新（规范 §5.3）

签名 URL 有效期 1 小时。`PlayerViewModel` 在调用 `audioPlayerManager.load()` 前检查到期时间；若接近到期（< 60s），先重新拉取 `Work` 刷新 URL：

```kotlin
// ui/work/PlayerViewModel.kt
fun playWork(work: Work) {
    viewModelScope.launch {
        val freshWork = if (isExpiringSoon(work.audioUrlExpiresAt)) {
            // 接近到期，重新获取完整 Work 并更新 ViewModel 状态（规范 §5.3）
            // 只抽取 audioUrl 会导致 audioUrlExpiresAt 仍为旧值，下次判断失效
            workRepository.getWork(work.id).also { _currentWork.value = it }
        } else {
            work
        }
        audioPlayerManager.load(freshWork.audioUrl)
        audioPlayerManager.play()
    }
}

private fun isExpiringSoon(expiresAt: Instant): Boolean =
    Instant.now().isAfter(expiresAt.minusSeconds(60))
```

> `audioUrlExpiresAt` 来自 `Work` 数据结构（规范 §5.2 的 `audio_url_expires_at` 字段），由 `WorkRepository` 映射至本地 `Work` 模型，UI 层无需直接处理签名逻辑。

---

### 6.6 Compose 中的状态收集与生命周期安全

```kotlin
// Screen 层：用 collectAsStateWithLifecycle 而非 collectAsState
// collectAsStateWithLifecycle 在 STOPPED 时自动停止收集，避免后台资源消耗
@Composable
fun TaskProgressScreen(
    viewModel: TaskViewModel = hiltViewModel(),
) {
    val taskState by viewModel.taskState.collectAsStateWithLifecycle()
    // ...
}

// ViewModel 层：一次性事件（导航、Toast）用 SharedFlow + LaunchedEffect 消费
// 不要用 StateFlow 承载一次性事件——重组会重复消费
@Composable
fun LoginScreen(viewModel: AuthViewModel = hiltViewModel()) {
    val navController = LocalNavController.current
    LaunchedEffect(Unit) {
        viewModel.events.collect { event ->
            when (event) {
                AuthEvent.LoginSuccess -> navController.navigate("home") { popUpTo("login") }
                AuthEvent.SessionExpired -> navController.navigate("login") { popUpTo(0) }
            }
        }
    }
}
```

**依赖：** `collectAsStateWithLifecycle` 来自 `androidx.lifecycle:lifecycle-runtime-compose`（随 Compose BOM）。

---

## 7. 与 client-sdk-spec.md 接口对应

| SDK 接口（client-sdk-spec.md） | Android 实现类 | 方法 |
|---|---|---|
| `AuthSDK.register` | `AuthRepository.register` | → `AuthApi.register` |
| `AuthSDK.login` | `AuthRepository.login` | → `AuthApi.login` |
| `AuthSDK.refreshToken` | `TokenAuthenticator` | 自动触发，无需手动调用 |
| `AuthSDK.logout` | `AuthRepository.logout` | → `AuthApi.logout` + `TokenStorage.clear()` |
| `TaskSDK.createTask` | `TaskRepository.createTask` | → `TaskApi.createTask` |
| `TaskSDK.getTaskStatus` | `TaskRepository.getTask` | → `TaskApi.getTask` |
| `TaskSDK.watchTask` | `TaskRepository.watchTask` | 返回 `CancelHandle`；内部 WS → 轮询降级 → 210s 超时（见 §6.3） |
| `TaskSDK.cancelWatch` | `CancelHandle.cancel()` | job 取消触发 `awaitClose` 关闭该 task 的 WS 连接 |
| `WorkSDK.selectCandidate` | `WorkRepository.selectCandidate` | → `WorkApi.createWork` |
| `WorkSDK.listWorks` | `WorkRepository.listWorks` | → `WorkApi.listWorks` |
| `WorkSDK.getWork` | `WorkRepository.getWork` | → `WorkApi.getWork` |
| `WorkSDK.updateWork` | `WorkRepository.updateWork` | → `WorkApi.updateWork`（PATCH） |
| `WorkSDK.deleteWork` | `WorkRepository.deleteWork` | → `WorkApi.deleteWork`（DELETE） |
| `WorkSDK.getDownloadURL` | `WorkRepository.getDownloadUrl` | → `WorkApi.getDownloadUrl` |
| `CreditSDK.getCredits` | `CreditRepository.getCredits` | → `CreditApi.getCredits` |
| `AudioPlayer.load` | `AudioPlayerManager.load` | ExoPlayer.prepare |
| `AudioPlayer.play` | `AudioPlayerManager.play` | ExoPlayer.play |
| `AudioPlayer.pause` | `AudioPlayerManager.pause` | ExoPlayer.pause |
| `AudioPlayer.seek` | `AudioPlayerManager.seek(positionSeconds)` | ExoPlayer.seekTo(positionSeconds * 1000L) |
| `AudioPlayer.setVolume` | `AudioPlayerManager.setVolume` | ExoPlayer.volume |
| `AudioPlayer.release` | `AudioPlayerManager.release` | ExoPlayer.release |

---

## 8. 错误处理

参照 `client-sdk-spec.md §8` 错误码映射，在 `ErrorHandler.kt` 集中转换：

| API 错误码 | Android 处理 |
|---|---|
| `UNAUTHORIZED` (401) | `TokenAuthenticator` 自动刷新；刷新失败 → 发 `AuthEvent.SessionExpired` → 跳登录页 |
| `INSUFFICIENT_CREDITS` (402) | 弹 Dialog 提示配额已用完 |
| `CONTENT_FILTERED` (400) | 输入框下方显示过滤提示 |
| `QUEUE_FULL` (429) | Snackbar 提示"服务繁忙，请稍后再试" |
| `RATE_LIMIT_EXCEEDED` (429) | 同上，附加倒计时 |
| 5xx | Toast 提示"服务器异常，请稍后重试"，不清除本地状态 |
| 网络异常 | Snackbar 提示"网络不可用"；`watchTask` 的 `WsTransport` 失败后自动降级轮询 |
