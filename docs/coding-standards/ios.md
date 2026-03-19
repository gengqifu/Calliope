# iOS 编码规范

> 适用范围：`ios/` 目录下所有 Swift 代码
> 架构详情见 `docs/architecture/client-ios.md`

---

## 工具链

| 工具 | 版本 | 用途 |
|--|--|--|
| Swift | 5.10+ | 语言 |
| Xcode | 15+ | IDE 和构建 |
| SwiftFormat | 0.53+ | 代码格式化 |
| SwiftLint | 0.54+ | 静态分析 |
| XCTest | 内置 | 测试框架 |

### SwiftLint 配置（.swiftlint.yml）

```yaml
disabled_rules:
  - trailing_whitespace

opt_in_rules:
  - empty_count
  - explicit_init
  - first_where
  - force_unwrapping

line_length: 120
function_body_length:
  warning: 40
  error: 60
type_body_length:
  warning: 200
```

---

## 目录结构规范

```
ios/Calliope/
├── App/
│   ├── AppDelegate.swift
│   └── SceneDelegate.swift
├── UI/
│   ├── Auth/
│   │   ├── LoginViewController.swift
│   │   └── LoginViewModel.swift
│   ├── Task/
│   │   ├── CreateTaskViewController.swift
│   │   └── CreateTaskViewModel.swift
│   ├── Work/
│   │   ├── WorkListViewController.swift
│   │   ├── WorkListViewModel.swift
│   │   └── PlayerViewModel.swift
│   └── Common/
│       └── BaseViewController.swift
├── Domain/
│   └── UseCase/            # 可选
├── Data/
│   ├── Repository/
│   │   ├── AuthRepository.swift        # protocol
│   │   ├── AuthRepositoryImpl.swift
│   │   ├── TaskRepository.swift
│   │   └── WorkRepository.swift
│   ├── Remote/
│   │   ├── APIClient.swift             # URLSession 封装
│   │   ├── APIEndpoint.swift           # endpoint enum
│   │   └── DTO/                        # Codable 结构体
│   └── Local/
│       └── KeychainHelper.swift
├── Network/
│   ├── TokenRefresher.swift            # actor，防并发刷新
│   └── WebSocketManager.swift
├── Audio/
│   └── AudioPlayerManager.swift        # AVPlayer 封装
└── DI/
    └── AppContainer.swift              # 手动依赖注入容器
```

---

## 命名规范

| 类别 | 规范 | 示例 |
|--|--|--|
| 类型（class/struct/enum/protocol）| PascalCase | `TaskRepository` |
| 函数/变量/属性 | camelCase | `createTask()` |
| 常量 | camelCase（Swift 惯例）| `maxRetryCount` |
| 枚举 case | camelCase | `.idle`、`.playing` |
| Protocol | 能力或角色命名 | `Playable`、`TaskCreating` |
| 私有属性 | 无需下划线前缀（用 `private` 修饰）| `private var client` |

---

## 架构约束

### ViewModel 只暴露 @Published 属性

```swift
// ✅ 正确
final class LoginViewModel: ObservableObject {
    @Published private(set) var uiState: LoginUiState = .idle
    @Published private(set) var isLoading: Bool = false
}

// ❌ 错误：暴露可写 @Published
@Published var uiState: LoginUiState = .idle  // 外部可直接修改
```

### UI 更新必须在主线程

```swift
// ✅ 使用 @MainActor 标注 ViewModel
@MainActor
final class LoginViewModel: ObservableObject {
    func login(email: String, password: String) async {
        isLoading = true  // 自动在主线程
        defer { isLoading = false }
        // ...
    }
}

// ✅ 网络回调切回主线程
Task { @MainActor in
    self.uiState = .success(result)
}

// ❌ 错误：在后台线程更新 UI（崩溃风险）
DispatchQueue.global().async {
    self.uiState = .success(result)  // ❌
}
```

### Repository 是唯一数据来源（Protocol）

```swift
// ✅ Protocol 定义接口，ViewModel 依赖 Protocol
protocol TaskRepository {
    func createTask(prompt: String) async throws -> Task
    func getTask(id: String) async throws -> Task
}

// ✅ ViewModel 依赖注入
final class CreateTaskViewModel: ObservableObject {
    private let taskRepo: TaskRepository  // protocol，便于测试 mock

    init(taskRepo: TaskRepository = AppContainer.shared.taskRepository) {
        self.taskRepo = taskRepo
    }
}
```

---

## Swift Concurrency 规范

```swift
// ✅ async/await 替代回调
func fetchWork(id: String) async throws -> Work {
    let (data, response) = try await URLSession.shared.data(from: url)
    guard let http = response as? HTTPURLResponse, http.statusCode == 200 else {
        throw APIError.badStatus
    }
    return try JSONDecoder().decode(Work.self, from: data)
}

// ✅ actor 防止数据竞争
actor TokenRefresher {
    private var refreshTask: Task<TokenPair, Error>?

    func refresh() async throws -> TokenPair {
        if let task = refreshTask { return try await task.value }
        let task = Task { try await performRefresh() }
        refreshTask = task
        defer { refreshTask = nil }
        return try await task.value
    }
}

// ❌ 禁止使用 DispatchQueue 做业务并发（改用 async/await）
DispatchQueue.global().async { ... }  // ❌（网络请求等 IO 场景）
```

---

## 错误处理规范

```swift
// ✅ 自定义错误类型
enum APIError: Error, LocalizedError {
    case badStatus(code: Int)
    case tokenExpired
    case networkError(underlying: Error)

    var errorDescription: String? {
        switch self {
        case .badStatus(let code): return "HTTP \(code)"
        case .tokenExpired: return "Token expired"
        case .networkError(let e): return e.localizedDescription
        }
    }
}

// ✅ ViewModel 捕获错误更新状态
func loadWorks() async {
    do {
        let works = try await workRepo.getWorks()
        uiState = .success(works)
    } catch {
        uiState = .failure(error.localizedDescription)
    }
}

// ❌ 禁止 try! 和 force unwrap（除非绝对安全且加注释）
let data = try! JSONEncoder().encode(model)  // ❌
```

---

## 资源管理规范

```swift
// ✅ Notification 观察者必须在 release() 或 deinit 中移除
func release() {
    NotificationCenter.default.removeObserver(
        self,
        name: .AVPlayerItemDidPlayToEndTime,
        object: nil
    )
    player?.pause()
    player = nil
}

// ✅ Task 在 deinit 中取消
deinit {
    cancellableTask?.cancel()
}
```

---

## 测试规范

```swift
// ✅ Mock Repository
final class MockTaskRepository: TaskRepository {
    var stubbedTask: Task?
    var thrownError: Error?

    func createTask(prompt: String) async throws -> Task {
        if let error = thrownError { throw error }
        return stubbedTask!
    }
}

// ✅ async 测试
func testCreateTaskSuccess() async throws {
    let mockRepo = MockTaskRepository()
    mockRepo.stubbedTask = Task(id: "1", status: .pending)
    let viewModel = CreateTaskViewModel(taskRepo: mockRepo)

    await viewModel.submit(prompt: "jazz music")

    XCTAssertEqual(viewModel.uiState, .success)
}
```

### 覆盖率要求

- ViewModel 层 100%
- Repository 层 > 80%
- ViewController UI 逻辑不强制覆盖
