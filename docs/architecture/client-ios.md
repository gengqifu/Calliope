# iOS 客户端架构设计

> 版本：v1.0
> 更新日期：2026-03-17
> 适用平台：iOS 15+（Swift 5.9+）

---

## 1. 概述

Calliope iOS 客户端采用 **MVVM + Swift Concurrency**，以 URLSession 作为唯一网络依赖，零第三方运行时库。SwiftUI 驱动 UI，`@Published` + `ObservableObject` 管理视图状态，`async/await` + `Actor` 处理并发安全。本文档描述代码组织方式、依赖选型、关键实现机制，以及与 `client-sdk-spec.md` 中各 SDK 接口的类对应关系。

---

## 2. 架构总览

```
┌─────────────────────────────────────────────────────────┐
│                     UI Layer                             │
│   SwiftUI View   ←→   ViewModel（ObservableObject）      │
│   @StateObject / @EnvironmentObject 注入                 │
└──────────────────────┬──────────────────────────────────┘
                       │ 调用 async 方法
┌──────────────────────▼──────────────────────────────────┐
│                   Domain Layer（可选）                    │
│   UseCase：封装 Repository 组合调用                       │
└──────────────────────┬──────────────────────────────────┘
                       │ 调用
┌──────────────────────▼──────────────────────────────────┐
│                    Data Layer                            │
│   Repository：聚合 Remote + Local，屏蔽数据来源            │
│   APIClient：URLSession 封装，统一错误处理 + Token 注入    │
│   KeychainHelper：AT/RT 安全存储                          │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│                  Network / Platform                      │
│   TokenRefresher（Actor）：并发安全的 Token 刷新          │
│   WsTransport：per-task URLSessionWebSocketTask 封装      │
│   AVPlayer：音频流式播放                                   │
└─────────────────────────────────────────────────────────┘
```

---

## 3. 分层职责

| 层 | 类/目录 | 职责 | 禁止 |
|---|---|---|---|
| UI Layer | `UI/*View`, `UI/*ViewModel` | 展示状态、处理用户手势、生命周期管理 | 直接网络调用 |
| Domain Layer | `Domain/UseCase/` | 组合 Repository 操作，封装业务规则 | 依赖 UIKit/SwiftUI |
| Data Layer | `Data/Repository/` | 决定数据来源，缓存策略 | 包含 UI 逻辑 |
| Remote | `Data/Remote/` | URLSession 请求、DTO 编解码 | 业务判断 |
| Local | `Data/Local/` | Keychain 读写 | 网络调用 |
| Network | `Network/` | Token 刷新、per-task WS 连接 | 业务逻辑 |

---

## 4. 依赖选型

| 用途 | 方案 | 说明 |
|---|---|---|
| UI 框架 | SwiftUI | iOS 15+ 全面支持，声明式，与 Combine/@Published 深度集成 |
| 网络 | URLSession + async/await | 零依赖，iOS 15 原生支持 async/await |
| JSON 序列化 | Codable（内置） | 编译期类型安全，无需第三方 |
| 异步 | Swift Concurrency（async/await + Actor） | iOS 15+，结构化并发，替代 Combine 复杂链式调用 |
| WebSocket | URLSessionWebSocketTask | iOS 13+，URLSession 内置，无需第三方 |
| 音频播放 | AVPlayer（AVFoundation） | 系统内置，支持 HLS 流式播放、进度观察 |
| 安全存储 | Security.framework（Keychain） | 系统内置，加密存储 AT/RT |
| 依赖注入 | 手动 DI（AppContainer） | MVP 阶段复杂度低，无需 Swinject 等第三方 |
| 导航 | NavigationStack（SwiftUI） | iOS 16+；iOS 15 降级用 NavigationView |

> **为什么不用 Alamofire / Combine？**
> Alamofire 解决的是 URLSession 的历史痛点（回调地狱），async/await 已原生解决；Combine 链式操作在 Swift Concurrency 出现后优势减弱，混用反而增加认知负担。MVP 阶段保持零运行时依赖，降低维护成本。

---

## 5. 目录结构

```
Calliope/
│
├── App/
│   ├── CalliopeApp.swift           # @main，注入 AppContainer
│   └── AppContainer.swift          # 手动 DI：组装所有依赖，作为 @EnvironmentObject 注入
│
├── UI/
│   ├── Auth/
│   │   ├── LoginView.swift
│   │   ├── RegisterView.swift
│   │   └── AuthViewModel.swift     # 对应 AuthSDK：login / register / logout
│   ├── Task/
│   │   ├── CreateTaskView.swift
│   │   ├── TaskProgressView.swift
│   │   └── TaskViewModel.swift     # 对应 TaskSDK：createTask / watchTask
│   ├── Work/
│   │   ├── WorkListView.swift
│   │   ├── WorkDetailView.swift
│   │   ├── WorkViewModel.swift     # 对应 WorkSDK：listWorks / selectCandidate
│   │   └── PlayerViewModel.swift  # 对应 AudioPlayer：play / pause / seek
│   └── Common/
│       ├── LoadingView.swift
│       ├── ErrorView.swift
│       └── ErrorMapper.swift      # API 错误码 → 用户提示
│
├── Domain/
│   └── UseCase/                   # MVP 阶段可选
│       ├── CreateTaskUseCase.swift
│       └── SelectCandidateUseCase.swift
│
├── Data/
│   ├── Repository/
│   │   ├── AuthRepository.swift
│   │   ├── TaskRepository.swift
│   │   ├── WorkRepository.swift
│   │   └── CreditRepository.swift
│   ├── Remote/
│   │   ├── APIClient.swift        # URLSession 封装：注入 AT、统一错误处理、401 触发刷新
│   │   ├── APIEndpoint.swift      # 所有端点定义（enum + URLRequest 构建）
│   │   └── DTO/
│   │       ├── AuthDTO.swift
│   │       ├── TaskDTO.swift
│   │       ├── WorkDTO.swift
│   │       └── ErrorDTO.swift
│   └── Local/
│       └── KeychainHelper.swift   # AT/RT/ExpiresAt 的 Keychain 读写封装
│
├── Network/
│   ├── TokenRefresher.swift       # Actor：并发安全的 Token 刷新，防多次并发刷新
│   └── WsTransport.swift          # per-task URLSessionWebSocketTask：每次 watchTask 独立连接
│
└── Audio/
    └── AudioPlayerManager.swift   # AVPlayer 封装，@Published 暴露 PlayerState / currentSeconds
```

---

## 6. 关键实现机制

### 6.1 Token 自动刷新（Actor 保证并发安全）

```swift
// Network/TokenRefresher.swift
actor TokenRefresher {
    private let keychain: KeychainHelper
    private let authEndpoint: String
    private var refreshTask: Task<String, Error>?  // 复用进行中的刷新任务

    init(keychain: KeychainHelper, authEndpoint: String) {
        self.keychain = keychain
        self.authEndpoint = authEndpoint
    }

    func validAccessToken() async throws -> String {
        // 如果 AT 未过期（提前 60s 判断），直接返回
        if let at = keychain.accessToken, !isExpiringSoon(keychain.expiresAt) {
            return at
        }
        // 如果已有刷新任务在进行，复用它（防止并发多次刷新）
        if let ongoing = refreshTask {
            return try await ongoing.value
        }
        return try await startRefreshTask()
    }

    /// 跳过 isExpiringSoon 检查，直接执行刷新（用于 APIClient 401 兜底重试）
    /// 与 validAccessToken() 的唯一区别是不做 isExpiringSoon 判断；
    /// 单飞语义保留：若已有刷新任务在飞，复用它（并发 401 共用同一次刷新，不再多发请求）
    func forceRefresh() async throws -> String {
        if let ongoing = refreshTask {
            return try await ongoing.value
        }
        return try await startRefreshTask()
    }

    private func startRefreshTask() async throws -> String {
        let task = Task<String, Error> {
            defer { Task { await self.clearRefreshTask() } }
            guard let rt = keychain.refreshToken else {
                throw AuthError.sessionExpired
            }
            let result = try await callRefreshAPI(rt: rt)
            // TokenPair 只含 accessToken + expiresAt，后端不轮换 RT（client-sdk-spec.md §3.1）
            keychain.saveAccessToken(result.accessToken, expiresAt: result.expiresAt)
            return result.accessToken
        }
        refreshTask = task
        return try await task.value
    }

    private func clearRefreshTask() { refreshTask = nil }
}

// Data/Remote/APIClient.swift
class APIClient {
    private let session = URLSession.shared
    private let tokenRefresher: TokenRefresher

    func request<T: Decodable>(_ endpoint: APIEndpoint) async throws -> T {
        let token = try await tokenRefresher.validAccessToken()
        var urlRequest = endpoint.urlRequest
        urlRequest.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")

        let (data, response) = try await session.data(for: urlRequest)
        guard let httpResponse = response as? HTTPURLResponse else {
            throw APIError.invalidResponse
        }

        if httpResponse.statusCode == 401 {
            // AT 在请求途中过期；刷新后重试一次
            // 仅"刷新失败"或"重试仍 401"才视为会话失效
            // 重试返回 402/429 等业务错误时继续按普通 API 响应处理，不能误触发登出
            guard let newToken = try? await tokenRefresher.forceRefresh() else {
                // RT 无效或网络错误，刷新失败 → 会话失效
                NotificationCenter.default.post(name: .sessionExpired, object: nil)
                throw AuthError.sessionExpired
            }
            var retryRequest = endpoint.urlRequest
            retryRequest.setValue("Bearer \(newToken)", forHTTPHeaderField: "Authorization")
            let (retryData, retryResponse) = try await session.data(for: retryRequest)
            guard let retryHTTP = retryResponse as? HTTPURLResponse else {
                throw APIError.invalidResponse
            }
            if retryHTTP.statusCode == 401 {
                // 刷新成功但重试仍 401 → RT 也已失效
                NotificationCenter.default.post(name: .sessionExpired, object: nil)
                throw AuthError.sessionExpired
            }
            // 重试返回业务错误（402/429/5xx 等），按普通响应处理
            guard (200..<300).contains(retryHTTP.statusCode) else {
                let errorDTO = try JSONDecoder().decode(ErrorDTO.self, from: retryData)
                throw APIError.serverError(code: errorDTO.code, message: errorDTO.message)
            }
            return try JSONDecoder().decode(T.self, from: retryData)
        }

        guard (200..<300).contains(httpResponse.statusCode) else {
            let errorDTO = try JSONDecoder().decode(ErrorDTO.self, from: data)
            throw APIError.serverError(code: errorDTO.code, message: errorDTO.message)
        }

        return try JSONDecoder().decode(T.self, from: data)
    }
}
```

**要点：**
- `TokenRefresher` 是 `actor`，天然串行化——多个并发请求同时触发刷新时，只有第一个发起网络请求，后续请求 `await` 同一个 `Task`
- `isExpiringSoon`：提前 60 秒判断过期，避免请求途中 AT 过期
- `validAccessToken()` 是主动刷新路径（请求前调用）；`APIClient` 遇到 401 时调用 `forceRefresh()`（忽略 `isExpiringSoon` 判断，直接执行刷新）并重试一次原请求，重试仍 401 才广播登出事件
- 刷新失败时通过 `NotificationCenter` 广播 `sessionExpired`，ViewModel 监听后跳登录页

---

### 6.2 per-task WS 连接（WsTransport）

规范要求每次 `watchTask` 使用独立连接 `wss://...?token=...&task_id={taskId}`（client-sdk-spec.md §4.2）。iOS 通过 `WsTransport` 为每个 taskId 打开独立的 `URLSessionWebSocketTask`，**不存在全局共享 WS 连接**。

```swift
// Network/WsTransport.swift
// 每次 watchTask 调用都建立独立的 per-task WS 连接
class WsTransport {
    private let session: URLSession

    init(session: URLSession = .shared) { self.session = session }

    /// 为指定 task 打开独立 WS 通道，返回 AsyncThrowingStream<TaskStatus, Error>
    /// 流在收到 completed/failed 终态时正常结束（finish()）
    /// WS 连接异常时以 finish(throwing:) 抛出错误，调用方可据此区分"正常结束"和"连接失败"
    func openTaskChannel(taskId: Int64, token: String) -> AsyncThrowingStream<TaskStatus, Error> {
        AsyncThrowingStream { continuation in
            let url = URL(string:
                "wss://api.calliope-music.com/ws?token=\(token)&task_id=\(taskId)")!
            let wsTask = session.webSocketTask(with: url)
            wsTask.resume()

            // Task 取消时（cancelWatch）关闭该 task 的独立 WS
            continuation.onTermination = { _ in
                wsTask.cancel(with: .normalClosure, reason: nil)
            }

            Task {
                do {
                    while !Task.isCancelled {
                        let msg = try await wsTask.receive()
                        if case .string(let text) = msg {
                            let status = parseTaskStatus(text)
                            continuation.yield(status)
                            if status.status == "completed" || status.status == "failed" {
                                continuation.finish()  // 正常结束（终态已收到）
                                return
                            }
                        }
                    }
                } catch {
                    // 连接失败/断开：以 throwing 结束，让调用方可区分"正常完成"与"连接异常"
                    continuation.finish(throwing: error)
                }
            }
        }
    }
}
```

**watchTask / cancelWatch 实现：**

```swift
// UI/Task/TaskViewModel.swift
typealias CancelHandle = () -> Void
private var wsTransport: WsTransport  // 注入

func watchTask(
    taskId: Int64,
    onUpdate: @escaping (TaskStatus) -> Void,
    onError: @escaping (Error) -> Void
) -> CancelHandle {
    let task = Task {
        do {
            // 规范 §4.4：210s 总超时
            try await withTimeout(seconds: 210) {
                let wsConsumed = try await tryWatchViaWs(taskId: taskId, onUpdate: onUpdate)
                if !wsConsumed {
                    // WS 连接失败，降级轮询（间隔 3s，规范 §4.3）
                    try await pollUntilTerminal(taskId: taskId, onUpdate: onUpdate)
                }
            }
        } catch is CancellationError {
            // cancelWatch 正常取消，不回调 onError
        } catch {
            onError(error)
        }
    }
    return { task.cancel() }  // cancelWatch 只取消当前 task 的监听
}

/// 返回 true = WS 正常消费至终态（stream 正常 finish）
/// 返回 false = WS 连接异常（stream finish(throwing:) 抛出），触发降级轮询
private func tryWatchViaWs(taskId: Int64, onUpdate: @escaping (TaskStatus) -> Void) async throws -> Bool {
    guard let token = keychain.accessToken else { return false }
    do {
        for try await status in wsTransport.openTaskChannel(taskId: taskId, token: token) {
            try Task.checkCancellation()  // 主动取消立即退出，不走降级路径
            onUpdate(status)
        }
        return true   // 流正常结束（终态已收到）
    } catch is CancellationError {
        throw CancellationError()  // 取消必须放行，不能误判为 WS 失败
    } catch {
        return false  // WS 连接异常（finish(throwing:) 传来），降级轮询
    }
}

private func pollUntilTerminal(taskId: Int64, onUpdate: @escaping (TaskStatus) -> Void) async throws {
    var lastStatus: String? = nil
    while true {
        try Task.checkCancellation()
        let status = try await taskApi.getTask(taskId)
        if status.status != lastStatus {
            onUpdate(status)
            lastStatus = status.status
        }
        if status.status == "completed" || status.status == "failed" { return }
        try await Task.sleep(nanoseconds: 3_000_000_000)
    }
}

// 对外暴露的使用示例
private var watchHandle: CancelHandle?

func startWatch(taskId: Int64) {
    watchHandle = watchTask(
        taskId: taskId,
        onUpdate: { [weak self] status in
            Task { @MainActor in self?.taskState = status.toViewState() }
        },
        onError: { [weak self] error in
            Task { @MainActor in self?.errorMessage = error.localizedDescription }
        }
    )
}

func stopWatch() { watchHandle?(); watchHandle = nil }
```

> **与 HTTP URLSession 的关系：** `WsTransport` 使用独立的 `URLSession.shared`，与 `APIClient` 的 HTTP 会话分开，WS 连接的生命周期完全由 `Task.cancel()` 驱动，通过 `continuation.onTermination` 确保 `wsTask.cancel()` 被调用。

---

### 6.3 音频播放状态机

```swift
// Audio/AudioPlayerManager.swift

// 对齐规范 §7.1 PlayerState：idle | loading | ready | playing | paused | ended | error
enum PlayerState { case idle, loading, ready, playing, paused, ended, error }

@MainActor
class AudioPlayerManager: ObservableObject {
    @Published var state: PlayerState = .idle
    // 进度以秒为单位，对齐规范 §7.1 onProgress(currentSeconds, totalSeconds)
    @Published var currentSeconds: Int = 0
    @Published var totalSeconds: Int = 0

    // §6.3.1 恢复逻辑所需：PlayerViewModel 通过这两个属性判断是否可重试
    private(set) var isLastErrorNetwork: Bool = false
    private(set) var currentUrl: URL? = nil

    private var player: AVPlayer?
    private var timeObserver: Any?
    private var statusObserver: NSKeyValueObservation?

    func load(url: URL) {
        // 重新加载前先清理旧观察者，防止重试路径下重复注册（资源泄漏 + 重复回调）
        if let observer = timeObserver { player?.removeTimeObserver(observer); timeObserver = nil }
        statusObserver?.invalidate(); statusObserver = nil
        NotificationCenter.default.removeObserver(self, name: .AVPlayerItemDidPlayToEndTime, object: nil)
        player = nil

        currentUrl = url
        isLastErrorNetwork = false
        let item = AVPlayerItem(url: url)
        player = AVPlayer(playerItem: item)
        state = .loading          // 对齐规范：load 完成前为 loading

        // 观察播放状态
        statusObserver = item.observe(\.status) { [weak self] item, _ in
            Task { @MainActor in
                switch item.status {
                case .readyToPlay:
                    let total = item.duration.seconds
                    self?.totalSeconds = total.isFinite ? Int(total) : 0
                    self?.state = .ready      // ready：已就绪，等待 play() 调用
                case .failed:
                    // 通过 NSError 域区分网络错误与非网络错误（规范 §7.3）
                    // 只有纯网络连接/超时错误才重试；404/格式不支持/解码失败不重试
                    if let nsErr = item.error as NSError?,
                       nsErr.domain == NSURLErrorDomain,
                       [NSURLErrorNotConnectedToInternet,
                        NSURLErrorTimedOut,
                        NSURLErrorNetworkConnectionLost].contains(nsErr.code) {
                        self?.isLastErrorNetwork = true
                    } else {
                        self?.isLastErrorNetwork = false
                    }
                    self?.state = .error
                default: break
                }
            }
        }

        // 进度更新（每 0.5 秒）；以秒为单位，对齐规范 §7.1
        timeObserver = player?.addPeriodicTimeObserver(
            forInterval: CMTime(seconds: 0.5, preferredTimescale: 600),
            queue: .main
        ) { [weak self] time in
            guard let self else { return }
            self.currentSeconds = time.seconds.isFinite ? Int(time.seconds) : 0
        }

        // 播放结束
        NotificationCenter.default.addObserver(
            self,
            selector: #selector(playerDidFinish),
            name: .AVPlayerItemDidPlayToEndTime,
            object: item
        )
    }

    func play()  { player?.play();  state = .playing }
    func pause() { player?.pause(); state = .paused }

    // 对齐规范 §7.1 seek(positionSeconds: Int)
    func seek(positionSeconds: Int) {
        let target = CMTime(seconds: Double(positionSeconds), preferredTimescale: 600)
        player?.seek(to: target)
    }

    // 对齐规范 §7.1 setVolume(0.0 ~ 1.0)
    func setVolume(_ volume: Float) {
        player?.volume = max(0, min(1, volume))
    }

    func release() {
        if let observer = timeObserver { player?.removeTimeObserver(observer) }
        statusObserver?.invalidate()
        NotificationCenter.default.removeObserver(self, name: .AVPlayerItemDidPlayToEndTime, object: nil)
        player = nil
        state = .idle
        currentSeconds = 0
        totalSeconds = 0
        currentUrl = nil
        isLastErrorNetwork = false
    }

    @objc private func playerDidFinish() {
        state = .ended   // 对齐规范：播放完毕为 ended，不是 idle
    }
}
```

---

### 6.3.1 音频网络错误自动恢复（规范 §7.3）

网络错误恢复职责由 `PlayerViewModel` 承担，`AudioPlayerManager` 只暴露 `isLastErrorNetwork`。恢复顺序：保存位置 → `load()` → 等待 `.ready` → `seek()` → `play()`（最多 3 次）。

```swift
// UI/Work/PlayerViewModel.swift
private var retryCount = 0
private let maxRetries = 3

init(...) {
    // 监听播放器状态变化
    Task { @MainActor in
        for await playerState in audioPlayerManager.$state.values {
            if playerState == .error { await handlePlaybackError() }
        }
    }
}

private func handlePlaybackError() async {
    guard audioPlayerManager.isLastErrorNetwork,     // 仅网络错误触发重试
          retryCount < maxRetries,
          let url = audioPlayerManager.currentUrl else {
        retryCount = 0
        return
    }
    let savedPosition = audioPlayerManager.currentSeconds
    retryCount += 1
    try? await Task.sleep(nanoseconds: 2_000_000_000)  // 等待 2s（规范 §7.3）

    audioPlayerManager.load(url: url)

    // 等待进入 ready 状态后再 seek + play（而不是 delay 猜测）
    for await playerState in audioPlayerManager.$state.values {
        if playerState == .ready {
            audioPlayerManager.seek(positionSeconds: savedPosition)
            audioPlayerManager.play()
            retryCount = 0  // 恢复成功，重置计数；下次独立故障从 0 开始重试
            break
        }
        if playerState == .error { break }  // 加载再次失败，停止等待（下次 error 事件会重入）
    }
}
```

> `$state.values` 是 `Published.Publisher` 桥接到 `AsyncSequence` 的标准方式（iOS 15+，系统 Combine 框架，无第三方依赖）。"等待 `.ready`"替代了固定 `sleep`，确保 `seek` 在播放器真正就绪后才执行。

---

### 6.4 @Published 线程安全

SwiftUI 要求 `@Published` 属性变更必须在主线程执行：

```swift
// 正确：Task { @MainActor in ... } 包裹网络回调
func loadWorks() {
    Task {
        let works = try await workRepository.listWorks(page: 1)
        await MainActor.run {           // 确保在主线程更新
            self.works = works
            self.isLoading = false
        }
    }
}

// 或者：在 ViewModel 类上标注 @MainActor
@MainActor
class WorkViewModel: ObservableObject {
    @Published var works: [Work] = []
    // 所有方法自动在主线程执行
}
```

---

### 6.5 audioUrl 到期刷新（规范 §5.3）

签名 URL 有效期 1 小时。`PlayerViewModel` 在调用 `audioPlayerManager.load()` 前检查到期时间；若接近到期（< 60s），先重新拉取完整 `Work` 刷新 URL 和过期时间：

```swift
// UI/Work/PlayerViewModel.swift
@MainActor
class PlayerViewModel: ObservableObject {
    private(set) var currentWork: Work?

    func playWork(_ work: Work) {
        Task {
            do {
                let freshWork: Work
                if isExpiringSoon(work.audioUrlExpiresAt) {
                    // 接近到期，重新获取完整 Work（含新签名 URL + 新 expiresAt）
                    // 只更新 URL 字段会导致 expiresAt 仍为旧值，下次检查失效
                    freshWork = try await workRepository.getWork(work.id)
                    currentWork = freshWork
                } else {
                    freshWork = work
                }
                await audioPlayerManager.load(url: URL(string: freshWork.audioUrl)!)
                await audioPlayerManager.play()
            } catch {
                // 刷新失败时降级：用原 URL 尝试播放
                await audioPlayerManager.load(url: URL(string: work.audioUrl)!)
                await audioPlayerManager.play()
            }
        }
    }

    private func isExpiringSoon(_ expiresAt: Date) -> Bool {
        expiresAt.timeIntervalSinceNow < 60
    }
}
```

> `audioUrlExpiresAt` 来自 `Work` 数据结构（规范 §5.2 的 `audio_url_expires_at` 字段），由 `WorkRepository` 映射至本地 `Work` 模型，UI 层无需直接处理签名逻辑。

---

## 7. 与 client-sdk-spec.md 接口对应

| SDK 接口（client-sdk-spec.md） | iOS 实现类 | 方法 |
|---|---|---|
| `AuthSDK.register` | `AuthRepository.register` | → `APIClient.request(.register)` |
| `AuthSDK.login` | `AuthRepository.login` | → `APIClient.request(.login)` |
| `AuthSDK.refreshToken` | `TokenRefresher.validAccessToken` | 自动触发，无需手动调用 |
| `AuthSDK.logout` | `AuthRepository.logout` | → `APIClient.request(.logout)` + `KeychainHelper.clear()` |
| `TaskSDK.createTask` | `TaskRepository.createTask` | → `APIClient.request(.createTask)` |
| `TaskSDK.getTaskStatus` | `TaskRepository.getTask` | → `APIClient.request(.getTask)` |
| `TaskSDK.watchTask` | `TaskViewModel.watchTask` | `WsTransport.openTaskChannel` per-task + 轮询降级，返回 `CancelHandle` |
| `TaskSDK.cancelWatch` | `CancelHandle()` | `task.cancel()` → `continuation.onTermination` 关闭该 task 的 WS |
| `WorkSDK.selectCandidate` | `WorkRepository.selectCandidate` | → `APIClient.request(.createWork)` |
| `WorkSDK.listWorks` | `WorkRepository.listWorks` | → `APIClient.request(.listWorks)` |
| `WorkSDK.getDownloadURL` | `WorkRepository.getDownloadUrl` | → `APIClient.request(.downloadUrl)` |
| `CreditSDK.getCredits` | `CreditRepository.getCredits` | → `APIClient.request(.getCredits)` |
| `AudioPlayer.load` | `AudioPlayerManager.load` | AVPlayer + AVPlayerItem |
| `AudioPlayer.play` | `AudioPlayerManager.play` | AVPlayer.play() |
| `AudioPlayer.pause` | `AudioPlayerManager.pause` | AVPlayer.pause() |
| `AudioPlayer.seek` | `AudioPlayerManager.seek(positionSeconds:)` | `CMTime(seconds:)` |
| `AudioPlayer.setVolume` | `AudioPlayerManager.setVolume(_:)` | `AVPlayer.volume` |
| `AudioPlayer.release` | `AudioPlayerManager.release` | AVPlayer = nil |

---

## 8. 错误处理

参照 `client-sdk-spec.md §8` 错误码映射，在 `ErrorMapper.swift` 集中转换：

| API 错误码 | iOS 处理 |
|---|---|
| `UNAUTHORIZED` (401) | `TokenRefresher` 自动刷新；失败 → `NotificationCenter.sessionExpired` → 返回登录页 |
| `INSUFFICIENT_CREDITS` (402) | Alert 提示配额已用完 |
| `CONTENT_FILTERED` (400) | 输入框下方 inline 错误提示 |
| `QUEUE_FULL` (429) | Alert 提示"服务繁忙，请稍后再试" |
| `RATE_LIMIT_EXCEEDED` (429) | 同上 |
| 5xx | Alert 提示"服务器异常"，不清除本地状态 |
| 网络异常 | Banner 提示"网络不可用"；watchTask 的 WsTransport 失败后自动降级轮询 |
