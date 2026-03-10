# 客户端 SDK 接口规范

> 版本：v1.0
> 更新日期：2026-03-09
> 适用平台：Android（Kotlin）、iOS（Swift）、H5（Vanilla JS）

---

## 1. 概述

本文档定义三端客户端需要封装的网络层、WebSocket 层、音频播放层的统一接口语义。各平台用各自语言实现，但对业务层暴露相同的能力模型。

**层次划分：**

```
业务/UI 层
    ↓
SDK 接口层（本文档定义）
    ↓  ↓  ↓
HTTP 客户端  WebSocket 客户端  音频播放器
    ↓
后端 API（OpenAPI 规范）
```

---

## 2. 全局配置

所有平台的 SDK 初始化时需要配置：

| 配置项 | 说明 | 默认值 |
|--------|------|--------|
| `baseURL` | API 基础地址 | `https://api.calliope-music.com/api/v1` |
| `wsURL` | WebSocket 地址 | `wss://api.calliope-music.com/ws` |
| `requestTimeout` | HTTP 请求超时 | 30 秒 |
| `taskPollInterval` | WebSocket 不可用时轮询间隔 | 3 秒 |
| `taskTimeout` | 任务等待超时（客户端侧） | 210 秒（服务端 180s + 30s 缓冲） |

---

## 3. 认证模块（AuthSDK）

### 3.1 接口定义

```
register(email, password, passwordConfirm) → AuthResult
login(email, password, rememberMe = false) → AuthResult
refreshToken(refreshToken) → TokenPair
logout() → void
```

### 3.2 数据结构

```
AuthResult {
  accessToken:   string       // JWT，有效期 15 分钟
  refreshToken:  string       // UUID，有效期 7 天（rememberMe=true 时 30 天）
  expiresAt:     timestamp    // accessToken 过期时间
  user: {
    id:          int64
    email:       string
    nickname:    string
    createdAt:   timestamp
  }
}

TokenPair {
  accessToken:   string       // 新 JWT
  expiresAt:     timestamp    // 新 accessToken 过期时间
  // 注：后端不返回新 refreshToken，原 Refresh Token 继续有效直到过期或主动登出
}
```

> `logout()` 无需传参：后端通过 JWT 中的 user_id 执行 `DEL calliope:auth:refresh:{user_id}`，SDK 只需携带当前 Access Token 发送请求即可。

### 3.3 Token 自动刷新

SDK 内部实现 Token 自动续期，业务层无感知：

```
每次 HTTP 请求前：
  if (now > expiresAt - 60s):   // 提前 60 秒刷新
    newTokenPair = refreshToken(currentRefreshToken)
    存储新 Token

收到 401 响应时：
  尝试刷新一次 Token，成功后重试原请求
  刷新失败（Refresh Token 也过期）→ 触发 onAuthExpired 回调，业务层跳转登录页
```

### 3.4 Token 本地存储

| 平台 | 存储位置 |
|------|---------|
| Android | `EncryptedSharedPreferences`（Android Keystore 加密） |
| iOS | `Keychain`（kSecAttrAccessibleWhenUnlockedThisDeviceOnly） |
| H5 | `localStorage`（MVP 简化；生产环境建议 HttpOnly Cookie） |

---

## 4. 任务模块（TaskSDK）

### 4.1 接口定义

```
createTask(prompt, lyrics?, mode) → TaskCreateResult
getTaskStatus(taskId) → TaskStatus
watchTask(taskId, onUpdate, onError) → CancelHandle
cancelWatch(handle) → void
```

### 4.2 数据结构

```
TaskCreateResult {
  taskId:         int64
  status:         "queued"
  queuePosition:  int      // 前方等待任务数
}

TaskStatus {
  taskId:         int64
  status:         "queued" | "processing" | "completed" | "failed"
  queuePosition:  int?     // 仅 queued 时有值
  progress:       int?     // 0-100，仅 processing 时有值（伪进度）
  candidates:     AudioCandidate[]?   // 仅 completed 时有值
  failReason:     string?  // 仅 failed 时有值
  createdAt:      timestamp
  completedAt:    timestamp?
}

AudioCandidate {
  index:           "a" | "b"
  url:             string   // 七牛云签名 URL，有效期 1 小时
  durationSeconds: int
}
```

### 4.3 watchTask 实现策略

`watchTask` 对业务层透明地处理 WebSocket 与轮询降级：

```
watchTask(taskId, onUpdate, onError):
  1. 尝试建立 WebSocket 连接
     WSS {wsURL}?token={accessToken}&task_id={taskId}

  2. WebSocket 连接成功：
     - 收到消息 → 解析 → 调用 onUpdate(TaskStatus)
     - 收到 completed/failed → 调用 onUpdate 后自动关闭连接
     - 连接断开 → 进入降级轮询

  3. WebSocket 连接失败或断开，降级为轮询：
     - 每 3 秒调用 getTaskStatus(taskId)
     - 结果变化时调用 onUpdate(TaskStatus)
     - status 为 completed/failed 时停止轮询

  4. 客户端超时（210 秒）：
     - 停止 WS/轮询
     - 调用 onError(TimeoutError)

  5. 返回 CancelHandle，业务层可主动取消监听（如页面销毁）
```

---

## 5. 作品模块（WorkSDK）

### 5.1 接口定义

```
selectCandidate(taskId, candidate, title?) → Work
listWorks(page, pageSize) → WorkList
getWork(workId) → Work
updateWork(workId, title) → Work
deleteWork(workId) → void
getDownloadURL(workId) → DownloadInfo
```

### 5.2 数据结构

```
Work {
  id:                int64
  title:             string
  prompt:            string
  mode:              "vocal" | "instrumental"
  audioUrl:          string     // 七牛云签名 URL，有效期 1 小时
  audioUrlExpiresAt: timestamp  // 来自 API 响应的 audio_url_expires_at 字段，SDK 层播放前检查（见 §5.3）
  durationSeconds:   int
  playCount:         int
  createdAt:         timestamp
}

WorkList {
  total:    int
  page:     int
  pageSize: int
  items:    Work[]
}

DownloadInfo {
  downloadUrl: string    // 含 attname 参数，触发浏览器/系统下载
  filename:    string    // "{title}.mp3"
  expiresIn:   int       // 秒
}
```

### 5.3 audioUrl 有效期处理

SDK 应在返回 Work 时附带过期时间，业务层播放前检查：

```
if (now > work.audioUrlExpiresAt - 60s):
  freshWork = getWork(work.id)    // 重新获取签名 URL
  play(freshWork.audioUrl)
else:
  play(work.audioUrl)
```

---

## 6. 额度模块（CreditSDK）

```
getCredits() → CreditInfo
```

```
CreditInfo {
  date:       string      // "2026-03-09"
  used:       int
  limit:      int
  remaining:  int
  resetsAt:   timestamp   // UTC+8 次日零点
}
```

---

## 7. 音频播放模块（AudioPlayer）

### 7.1 接口定义

```
load(url) → void
play() → void
pause() → void
seek(positionSeconds) → void
setVolume(0.0 ~ 1.0) → void
release() → void

// 状态回调
onStateChanged(state: "idle" | "loading" | "ready" | "playing" | "paused" | "ended" | "error")
onProgress(currentSeconds, totalSeconds)
onError(error)
```

### 7.2 平台实现映射

| 能力 | Android | iOS | H5 |
|------|---------|-----|----|
| 流式播放 | `ExoPlayer`（Media3） | `AVPlayer` | `<audio>` + Media Source |
| 后台播放 | `MediaSessionService` | `AVAudioSession` | 不支持（浏览器限制） |
| 进度保存 | `SharedPreferences` | `UserDefaults` | `sessionStorage` |
| 断点续播 | ExoPlayer 内置 | AVPlayer 内置 | 手动 `currentTime` 恢复 |

### 7.3 网络异常恢复（US-07 要求）

```
onError 收到网络错误时：
  1. 等待 2 秒
  2. 记录当前播放位置 (currentPosition)
  3. 重新 load(url)
  4. 播放就绪后 seek(currentPosition)
  5. play()
  最多重试 3 次，超过则调用 onError 通知业务层
```

---

## 8. 错误处理规范

### 8.1 统一错误类型

所有平台的 SDK 应将后端错误码映射为本地错误类型：

| 后端 code | 本地错误类型 | 业务层处理建议 |
|-----------|-------------|--------------|
| `UNAUTHORIZED` | `AuthError.tokenExpired` | 触发重新登录流程 |
| `INVALID_CREDENTIALS` | `AuthError.invalidCredentials` | 显示"邮箱或密码不正确" |
| `ACCOUNT_LOCKED` | `AuthError.accountLocked` | 显示"登录失败次数过多，账号已锁定 15 分钟" |
| `INSUFFICIENT_CREDITS` | `BusinessError.noCredits` | 显示"今日额度已用完" |
| `CONTENT_FILTERED` | `BusinessError.contentFiltered` | 提示用户修改输入 |
| `QUEUE_FULL` | `BusinessError.queueFull` | 提示稍后重试 |
| `RATE_LIMIT_EXCEEDED` | `NetworkError.rateLimited` | 显示"请求太频繁" |
| `NOT_FOUND` | `BusinessError.notFound` | 刷新列表 |
| `VALIDATION_ERROR` | `InputError` | 高亮错误字段 |
| `WORK_ALREADY_SAVED` | `BusinessError.alreadySaved` | 提示"该任务已保存过作品" |
| 网络超时/无连接 | `NetworkError.offline` | 显示网络状态提示 |
| 5xx | `ServerError` | 显示通用错误，记录日志 |

### 8.2 错误日志

SDK 应在本地记录 ERROR 级别的网络错误，包含：请求 URL、HTTP 状态码、后端 code、时间戳。MVP 阶段不上报到远端，开发时通过 Logcat / Xcode Console / 浏览器 DevTools 查看。

---

## 9. 平台特定注意事项

### Android

- **网络权限**：`AndroidManifest.xml` 需要 `INTERNET` 权限
- **HTTP 客户端**：推荐 `OkHttp` + `Retrofit`（JSON 序列化用 `kotlinx.serialization` 或 `Gson`）
- **WebSocket**：`OkHttp WebSocket`
- **最低版本**：Android API 26（Android 8.0）

### iOS

- **网络层**：推荐 `URLSession`（系统原生，无需第三方）或 `Alamofire`
- **WebSocket**：`URLSessionWebSocketTask`（iOS 13+，满足 iOS 15+ 要求）
- **ATS**：`Info.plist` 确保 HTTPS 域名已在 App Transport Security 白名单中（或默认全局 HTTPS 无需额外配置）
- **最低版本**：iOS 15

### H5

- **HTTP 客户端**：`fetch` API（原生，无需库）
- **WebSocket**：原生 `WebSocket` 对象
- **CORS**：后端已配置 `Access-Control-Allow-Origin`，前端直接调用即可
- **兼容性**：Chrome 90+、Safari 14+、Edge 90+（均原生支持 `fetch` 和 `WebSocket`）
- **音频自动播放**：Safari 要求用户有交互行为后才能调用 `audio.play()`，需在按钮点击事件中触发播放
