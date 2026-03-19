# Android 编码规范

> 适用范围：`android/` 目录下所有 Kotlin 代码
> 架构详情见 `docs/architecture/client-android.md`

---

## 工具链

| 工具 | 版本 | 用途 |
|--|--|--|
| Kotlin | 2.0+ | 语言 |
| Android Gradle Plugin | 8.4+ | 构建 |
| ktfmt | 0.47+ | 代码格式化（Google style）|
| detekt | 1.23+ | 静态分析 |
| JUnit 4 | 4.13+ | 单元测试 |
| Mockk | 1.13+ | Mock 框架 |

### detekt 配置（detekt.yml）

```yaml
style:
  MagicNumber:
    active: true
    ignoreNumbers: [0, 1, -1, 2]
complexity:
  LongMethod:
    threshold: 40
  CyclomaticComplexMethod:
    threshold: 10
naming:
  FunctionNaming:
    functionPattern: '[a-z][a-zA-Z0-9]*'
```

---

## 目录结构规范

```
android/app/src/main/java/com/calliope/
├── ui/
│   ├── auth/
│   │   ├── LoginFragment.kt
│   │   ├── LoginViewModel.kt
│   │   └── RegisterFragment.kt
│   ├── task/
│   │   ├── CreateTaskFragment.kt
│   │   └── CreateTaskViewModel.kt
│   ├── work/
│   │   ├── WorkListFragment.kt
│   │   ├── WorkListViewModel.kt
│   │   └── PlayerViewModel.kt
│   └── common/
│       └── BaseFragment.kt
├── domain/
│   └── usecase/            # 可选，复杂业务逻辑抽象
├── data/
│   ├── repository/
│   │   ├── AuthRepository.kt       # interface
│   │   ├── AuthRepositoryImpl.kt
│   │   ├── TaskRepository.kt
│   │   └── WorkRepository.kt
│   ├── remote/
│   │   ├── ApiService.kt           # Retrofit interface
│   │   └── dto/                    # 网络层 DTO
│   └── local/
│       └── TokenStorage.kt         # EncryptedSharedPreferences 封装
├── network/
│   ├── OkHttpClientFactory.kt
│   ├── TokenAuthenticator.kt       # 401 自动刷新
│   └── WebSocketManager.kt
├── audio/
│   └── AudioPlayerManager.kt       # ExoPlayer 封装
└── di/
    └── AppModule.kt                # Hilt Module
```

---

## 命名规范

| 类别 | 规范 | 示例 |
|--|--|--|
| 类/接口/对象 | PascalCase | `TaskRepository` |
| 函数/变量 | camelCase | `createTask()` |
| 常量（companion object）| UPPER_SNAKE | `MAX_RETRY_COUNT` |
| 布局文件 | snake_case，模块前缀 | `fragment_login.xml` |
| 资源 ID | snake_case，类型前缀 | `btn_submit`、`tv_title` |
| 私有成员 | 下划线前缀（可选，团队统一）| `_uiState` |

---

## 架构约束

### ViewModel 只暴露不可变状态

```kotlin
// ✅ 正确
class LoginViewModel : ViewModel() {
    private val _uiState = MutableStateFlow(LoginUiState())
    val uiState: StateFlow<LoginUiState> = _uiState.asStateFlow()
}

// ❌ 错误：暴露可变状态
val uiState = MutableStateFlow(LoginUiState())
```

### Fragment 只做 UI 渲染，不含业务逻辑

```kotlin
// ✅ 正确：Fragment 只观察状态和调用 ViewModel
viewLifecycleOwner.lifecycleScope.launch {
    repeatOnLifecycle(Lifecycle.State.STARTED) {
        viewModel.uiState.collect { state ->
            render(state)
        }
    }
}

// ❌ 错误：Fragment 直接调用 Repository 或操作网络
val response = retrofit.create(ApiService::class.java).login(...)
```

### Repository 是唯一数据来源

```kotlin
// ✅ ViewModel 通过 Repository 获取数据
class TaskViewModel(private val taskRepo: TaskRepository) : ViewModel() {
    fun loadTasks() = viewModelScope.launch {
        _uiState.update { it.copy(isLoading = true) }
        taskRepo.getTasks().fold(
            onSuccess = { tasks -> _uiState.update { it.copy(tasks = tasks, isLoading = false) } },
            onFailure = { e -> _uiState.update { it.copy(error = e.message, isLoading = false) } }
        )
    }
}
```

---

## 协程规范

```kotlin
// ✅ ViewModel 用 viewModelScope
viewModelScope.launch { ... }

// ✅ Fragment 用 repeatOnLifecycle（防止后台更新 UI）
repeatOnLifecycle(Lifecycle.State.STARTED) {
    flow.collect { ... }
}

// ❌ 禁止 GlobalScope
GlobalScope.launch { ... }  // ❌

// ❌ 禁止裸线程
Thread { ... }.start()  // ❌

// ✅ CancellationException 必须重抛
try {
    suspendFunction()
} catch (e: CancellationException) {
    throw e  // 必须！否则协程取消机制失效
} catch (e: Exception) {
    // 处理其他异常
}
```

---

## 错误处理规范

```kotlin
// ✅ Repository 返回 Result<T>
suspend fun login(req: LoginRequest): Result<TokenPair> = runCatching {
    apiService.login(req).toTokenPair()
}

// ✅ ViewModel 用 fold 处理
result.fold(
    onSuccess = { token -> _uiState.update { it.copy(token = token) } },
    onFailure = { e -> _uiState.update { it.copy(error = e.message) } }
)
```

---

## 测试规范

```kotlin
// ✅ ViewModel 单元测试
class LoginViewModelTest {
    @get:Rule val mainDispatcherRule = MainDispatcherRule()

    private val authRepo: AuthRepository = mockk()
    private lateinit var viewModel: LoginViewModel

    @Before
    fun setup() {
        viewModel = LoginViewModel(authRepo)
    }

    @Test
    fun `login success updates state`() = runTest {
        coEvery { authRepo.login(any()) } returns Result.success(fakeTokenPair)
        viewModel.login("user@example.com", "password")
        assertThat(viewModel.uiState.value.isLoggedIn).isTrue()
    }
}
```

### 覆盖率要求

- `ViewModel` 层 100%
- `Repository` 层 > 80%（含 Mock 网络层）
- `Fragment` UI 逻辑不强制覆盖

---

## 依赖注入规范（Hilt）

```kotlin
// ✅ Module 提供单例依赖
@Module
@InstallIn(SingletonComponent::class)
object NetworkModule {
    @Provides
    @Singleton
    fun provideOkHttpClient(tokenAuthenticator: TokenAuthenticator): OkHttpClient =
        OkHttpClientFactory.create(tokenAuthenticator)
}

// ❌ 禁止手动 new 依赖对象
val repo = TaskRepositoryImpl(RetrofitInstance.api)  // ❌
```
