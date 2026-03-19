# H5 客户端架构设计

> 版本：v1.0
> 更新日期：2026-03-17
> 适用平台：H5（原生 JS，Chrome 90+ / Safari 14+ / Edge 90+）

---

## 1. 概述

Calliope H5 客户端采用**模块化原生 JS（ES Modules，无框架）**，MVC 思路手动实现。零第三方运行时依赖，仅可选用 Vite 作为开发/打包工具。本文档描述模块组织方式、各层职责、关键实现机制，以及与 `client-sdk-spec.md` 中各 SDK 接口的函数对应关系。

> **为什么不用 React / Vue？** 本项目 H5 端定位为练习原生 JS，同时 MVP 页面数量少（登录 / 创建任务 / 进度 / 作品列表），引入框架带来的构建复杂度高于收益。

---

## 2. 架构总览

```
┌─────────────────────────────────────────────────────────┐
│                     UI Layer                             │
│   ui/auth.js  ui/task.js  ui/work.js                    │
│   DOM 操作、事件绑定、页面状态渲染                          │
└──────────────────────┬──────────────────────────────────┘
                       │ 调用
┌──────────────────────▼──────────────────────────────────┐
│                  Service Layer                           │
│   api/auth.js  api/task.js  api/work.js  api/credit.js  │
│   封装业务操作，对 UI 层屏蔽 HTTP/WS 细节                  │
└───────────────┬──────────────────────┬──────────────────┘
                │                      │
┌───────────────▼──────┐   ┌───────────▼──────────────────┐
│   api/client.js       │   │   ws/taskSocket.js           │
│   fetch 封装          │   │   per-task WS 连接            │
│   统一注入 AT          │   │   失败 → 轮询降级              │
│   401 → 刷新 → 重放   │   └──────────────────────────────┘
└───────────────┬──────┘
                │
┌───────────────▼──────────────────────────────────────────┐
│   storage/tokenStore.js                                  │
│   localStorage AT/RT 读写、过期检测                        │
└──────────────────────────────────────────────────────────┘

audio/player.js — <audio> 状态机，独立于 API 层
```

---

## 3. 模块职责

| 模块 | 文件 | 职责 | 禁止 |
|---|---|---|---|
| HTTP 客户端 | `api/client.js` | fetch 封装、AT 注入、401 自动刷新、重放原请求 | 业务逻辑 |
| 认证 | `api/auth.js` | register / login / logout / refresh 调用 | DOM 操作 |
| 任务 | `api/task.js` | createTask / getTask / watchTask（WS + 轮询降级 + 210s 超时） | DOM 操作 |
| 作品 | `api/work.js` | selectCandidate / listWorks / getDownloadUrl | DOM 操作 |
| 额度 | `api/credit.js` | getCredits | DOM 操作 |
| Token 存储 | `storage/tokenStore.js` | localStorage 读写、过期检测、刷新 | 网络调用 |
| WebSocket | `ws/taskSocket.js` | per-task WS 连接（每个 watchTask 独立连接）；失败降级轮询 | 业务逻辑 |
| 音频 | `audio/player.js` | `<audio>` 状态机封装、进度回调 | 网络调用 |
| UI 页面 | `ui/*.js` | DOM 渲染、事件绑定、调用 api/* 和 ws/* | 直接 fetch |
| 路由 | `main.js` | hash router，挂载/卸载页面模块 | 业务逻辑 |
| 通用组件 | `ui/components/*.js` | loadingSpinner / errorToast / dialog | — |

---

## 4. 依赖选型

| 用途 | 方案 | 说明 |
|---|---|---|
| HTTP | fetch API（内置） | Chrome 90+ / Safari 14+ 全面支持 |
| WebSocket | WebSocket API（内置） | 所有目标浏览器支持 |
| 音频播放 | `<audio>` 元素（内置） | 支持 MP3，流式加载；Safari 要求手势触发 play() |
| Token 存储 | localStorage（内置） | AT/RT 明文存储；可后续升级为 httpOnly Cookie |
| 模块系统 | ES Modules（type="module"） | 原生支持，无需打包工具（开发阶段） |
| 打包（可选） | Vite 5 | 生产构建，Tree-shaking，HMR |
| 序列化 | JSON.parse / JSON.stringify（内置） | — |

---

## 5. 目录结构

```
src/
│
├── api/
│   ├── client.js        # fetch 核心封装：AT 注入、401 → 刷新 → 重放、统一错误处理
│   ├── auth.js          # register(email,pw,pwConfirm) / login(email,pw) / logout()
│   ├── task.js          # createTask(params) / getTask(taskId)
│   ├── work.js          # selectCandidate(taskId,candidate) / listWorks(page) / getDownloadUrl(workId)
│   └── credit.js        # getCredits()
│
├── ws/
│   └── taskSocket.js    # openTaskChannel(taskId, token, onMessage, onError) → cancelFn
│                        # 每次调用建立独立 per-task WS；失败通过 onError 回调触发降级
│
├── audio/
│   └── player.js        # load(url) / play() / pause() / seek(positionSeconds) / setVolume(v) / release()
│                        # 暴露 onStateChange(cb) / onProgress(cb)（回调 {currentSeconds, totalSeconds}）
│
├── storage/
│   └── tokenStore.js    # getAccessToken() / getRefreshToken() / save(at,rt,exp) / clear() / refresh()
│                        # isExpiringSoon()：提前 60s 判断
│
├── ui/
│   ├── auth.js          # 登录页 / 注册页：表单验证、调用 api/auth.js、跳转
│   ├── task.js          # 创建任务页 / 进度页：调用 api/task.js（含 watchTask）
│   ├── work.js          # 作品列表页 / 详情页：调用 api/work.js + audio/player.js
│   └── components/
│       ├── loadingSpinner.js   # show() / hide()
│       ├── errorToast.js       # show(message, duration)
│       └── dialog.js           # confirm(message) → Promise<boolean>
│
├── main.js              # hash router（#/login #/create #/progress/:id #/works）
│                        # 挂载/卸载页面模块，注入全局依赖
└── index.html           # 单页入口，<script type="module" src="main.js">
```

---

## 6. 关键实现机制

### 6.1 Token 自动刷新（并发安全）

```javascript
// storage/tokenStore.js
let _refreshPromise = null;  // 进行中的刷新任务，防并发多次刷新

export function getAccessToken() {
  return localStorage.getItem('calliope_at');
}

export function isExpiringSoon() {
  const expiresAt = parseInt(localStorage.getItem('calliope_at_expires') || '0', 10);
  return Date.now() >= (expiresAt - 60_000);  // 提前 60s
}

export async function refresh() {
  if (_refreshPromise) return _refreshPromise;  // 复用进行中的刷新

  _refreshPromise = (async () => {
    const rt = localStorage.getItem('calliope_rt');
    if (!rt) throw new Error('SESSION_EXPIRED');

    const resp = await fetch('/api/v1/auth/refresh', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refreshToken: rt }),
    });

    if (!resp.ok) {
      clear();
      throw new Error('SESSION_EXPIRED');
    }

    const { accessToken, expiresAt } = await resp.json();
    // 规范 §3.1：TokenPair 只含 accessToken + expiresAt，后端不轮换 RT，不覆盖 RT
    localStorage.setItem('calliope_at', accessToken);
    localStorage.setItem('calliope_at_expires', String(new Date(expiresAt).getTime()));
    return accessToken;
  })().finally(() => { _refreshPromise = null; });

  return _refreshPromise;
}

export function save(at, rt, expiresAt) {
  localStorage.setItem('calliope_at', at);
  localStorage.setItem('calliope_rt', rt);
  localStorage.setItem('calliope_at_expires', String(new Date(expiresAt).getTime()));
}

export function clear() {
  ['calliope_at', 'calliope_rt', 'calliope_at_expires'].forEach(k => localStorage.removeItem(k));
}
```

```javascript
// api/client.js
import * as tokenStore from '../storage/tokenStore.js';

// 排队等待刷新的请求（并发保护）
let _pendingRequests = [];

export async function request(path, options = {}) {
  // 如果 AT 即将过期，先刷新
  if (tokenStore.isExpiringSoon()) {
    await tokenStore.refresh();
  }

  const at = tokenStore.getAccessToken();
  const headers = {
    'Content-Type': 'application/json',
    ...(at ? { Authorization: `Bearer ${at}` } : {}),
    ...options.headers,
  };

  const resp = await fetch(`/api/v1${path}`, { ...options, headers });

  if (resp.status === 401) {
    // AT 在请求途中失效，刷新后重发一次
    try {
      const newAt = await tokenStore.refresh();
      const retryResp = await fetch(`/api/v1${path}`, {
        ...options,
        headers: { ...headers, Authorization: `Bearer ${newAt}` },
      });
      if (retryResp.status === 401) {
        dispatchSessionExpired();
        throw new ApiError('UNAUTHORIZED', '登录已过期，请重新登录');
      }
      return parseResponse(retryResp);
    } catch (e) {
      if (e.message === 'SESSION_EXPIRED') dispatchSessionExpired();
      throw e;
    }
  }

  return parseResponse(resp);
}

function dispatchSessionExpired() {
  tokenStore.clear();
  window.dispatchEvent(new CustomEvent('calliope:sessionExpired'));
}

async function parseResponse(resp) {
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new ApiError(data.code || 'UNKNOWN', data.message || '请求失败', resp.status);
  return data;
}

export class ApiError extends Error {
  constructor(code, message, status) {
    super(message);
    this.code = code;
    this.status = status;
  }
}
```

---

### 6.2 per-task WebSocket + watchTask 编排（规范 §4.3）

规范要求每个 `watchTask` 调用建立独立连接，URL 携带 `task_id`；**不存在共享的全局 WS 连接**。

#### ws/taskSocket.js — 纯连接器

```javascript
// ws/taskSocket.js
// 职责：为单个 taskId 建立 WS 连接，通过回调传递消息/错误；返回 cancel 函数
// 不负责重试、超时、轮询；这些由 api/task.js 的 watchTask 编排

export function openTaskChannel(taskId, token, onMessage, onError) {
  const url = `wss://api.calliope-music.com/ws?token=${token}&task_id=${taskId}`;
  const ws = new WebSocket(url);
  let done = false;

  ws.onmessage = (event) => {
    if (done) return;
    try { onMessage(JSON.parse(event.data)); } catch (e) { /* ignore malformed */ }
  };

  ws.onerror = () => {
    if (done) return;
    done = true;
    onError(new Error('WS_ERROR'));
  };

  ws.onclose = (event) => {
    if (done) return;
    done = true;
    if (event.code !== 1000) {
      onError(new Error('WS_CLOSED'));   // 非正常关闭 → 触发降级轮询
    }
    // code 1000（cancel 触发）：done 已为 true，不会到达此处
  };

  return function cancel() {
    done = true;
    ws.close(1000, 'Watch cancelled');
  };
}
```

#### api/task.js — watchTask 编排（WS → 轮询降级 → 210s 超时）

```javascript
// api/task.js（新增）
import { openTaskChannel } from '../ws/taskSocket.js';
import { request } from './client.js';
import { getAccessToken } from '../storage/tokenStore.js';

// 返回 cancelFn；调用后立即停止监听（WS 关闭 + 轮询取消）
export function watchTask({ taskId, onUpdate, onError }) {
  let cancelled = false;
  let cancelWs = null;
  let pollTimer = null;

  // 规范 §4.4：210s 总超时
  const timeoutTimer = setTimeout(() => {
    cleanup();
    onError(new Error('TASK_TIMEOUT'));
  }, 210_000);

  function cleanup() {
    cancelled = true;
    cancelWs?.();
    cancelWs = null;
    clearTimeout(pollTimer);
    clearTimeout(timeoutTimer);
  }

  function isTerminal(status) {
    return status === 'completed' || status === 'failed';
  }

  function startPolling() {
    if (cancelled) return;
    pollTimer = setTimeout(async () => {
      if (cancelled) return;
      try {
        const task = await request(`/tasks/${taskId}`);
        if (cancelled) return;  // await 期间发生 cancel/timeout，丢弃本次结果
        onUpdate(task);
        if (isTerminal(task.status)) { cleanup(); return; }
        startPolling();  // 继续轮询（间隔 3s，规范 §4.3）
      } catch (e) {
        if (!cancelled) onError(e);
        cleanup();
      }
    }, 3_000);
  }

  // 先尝试 WS；onError 回调触发时降级为轮询
  cancelWs = openTaskChannel(
    taskId, getAccessToken(),
    (msg) => {
      if (cancelled) return;
      onUpdate(msg);
      if (isTerminal(msg.status)) cleanup();  // 终态：释放资源
    },
    (_err) => {
      // WS 连接失败或异常断开 → 降级轮询（规范 §4.3）
      cancelWs = null;
      if (!cancelled) startPolling();
    },
  );

  return function cancel() { cleanup(); };
}
```

> `cancelWatch` 对应 `watchTask` 返回的 `cancelFn`，调用后关闭该 task 的 WS（若仍连接），不影响其他任务的监听。

---

### 6.3 音频播放状态机

```javascript
// audio/player.js
// 状态枚举对齐规范 §7.1
const STATE = {
  IDLE: 'idle', LOADING: 'loading', READY: 'ready',
  PLAYING: 'playing', PAUSED: 'paused', ENDED: 'ended', ERROR: 'error',
};

let _audio = null;
let _state = STATE.IDLE;
let _onStateChange = null;
let _onProgress = null;   // 回调签名：({ currentSeconds, totalSeconds })

export function onStateChange(cb) { _onStateChange = cb; }
export function onProgress(cb) { _onProgress = cb; }

export function load(url) {
  release();
  _audio = new Audio();
  _audio.preload = 'auto';

  // canplay：缓冲足够可以播放 → READY（规范 §7.1）
  _audio.addEventListener('canplay',  () => _setState(STATE.READY));
  _audio.addEventListener('playing',  () => _setState(STATE.PLAYING));
  _audio.addEventListener('pause',    () => _setState(STATE.PAUSED));
  _audio.addEventListener('ended',    () => _setState(STATE.ENDED));
  _audio.addEventListener('error',    () => _setState(STATE.ERROR));
  // waiting：缓冲不足暂停播放 → LOADING
  _audio.addEventListener('waiting',  () => _setState(STATE.LOADING));
  _audio.addEventListener('timeupdate', () => {
    if (_audio && _audio.duration) {
      // 规范 §7.2：onProgress 回调 { currentSeconds, totalSeconds }
      _onProgress?.({ currentSeconds: _audio.currentTime, totalSeconds: _audio.duration });
    }
  });

  _setState(STATE.LOADING);
  _audio.src = url;
  _audio.load();
}

// Safari 要求 play() 必须在用户手势回调（click / touchend）中调用
// 如果从非手势上下文调用，play() 会返回被拒绝的 Promise
export async function play() {
  if (!_audio) return;
  try {
    await _audio.play();
  } catch (e) {
    // NotAllowedError：未在手势上下文，提示用户点击播放按钮
    if (e.name === 'NotAllowedError') {
      console.warn('[player] play() must be called from user gesture (Safari)');
      _setState(STATE.PAUSED);
    } else {
      _setState(STATE.ERROR);
    }
  }
}

export function pause() { _audio?.pause(); }

// 规范 §7.2：seek 参数为秒（整数），与移动端统一
export function seek(positionSeconds) {
  if (_audio && _audio.readyState >= HTMLMediaElement.HAVE_METADATA) {
    _audio.currentTime = positionSeconds;
  }
}

// 规范 §7.2：音量 0.0–1.0
export function setVolume(value) {
  if (_audio) _audio.volume = Math.max(0, Math.min(1, value));
}

export function release() {
  if (_audio) {
    _audio.pause();
    _audio.src = '';
    _audio = null;
  }
  _setState(STATE.IDLE);
}

function _setState(newState) {
  _state = newState;
  _onStateChange?.(newState);
}

export function getState() { return _state; }
```

---

### 6.4 audioUrl 到期刷新（规范 §5.3）

签名 URL 有效期 1 小时。`ui/work.js` 在调用 `player.load()` 前检查 `audioUrlExpiresAt`；若剩余时间 < 60s，先重新拉取 `Work` 刷新 URL，并以新的 `Work` 对象替换当前状态（防止旧过期时间残留）：

```javascript
// ui/work.js
import { getWork } from '../api/work.js';
import { load, play } from '../audio/player.js';

async function playWork(work) {
  let freshWork = work;
  if (isExpiringSoon(work.audioUrlExpiresAt)) {
    // 接近到期：重新获取完整 Work，更新状态（规范 §5.3）
    freshWork = await getWork(work.id);
    _currentWork = freshWork;  // 同步新的 audioUrlExpiresAt，避免下次误判
  }
  load(freshWork.audioUrl);
  // Safari：play() 须在手势回调中调用，此处假设已处于用户手势上下文
  await play();
}

function isExpiringSoon(expiresAt) {
  return Date.now() >= (new Date(expiresAt).getTime() - 60_000);  // 提前 60s
}
```

---

### 6.5 Hash Router（单页路由）

```javascript
// main.js
import { renderAuth }  from './ui/auth.js';
import { renderTask }  from './ui/task.js';
import { renderWorks } from './ui/work.js';

// 全局 sessionExpired 监听
window.addEventListener('calliope:sessionExpired', () => {
  window.location.hash = '#/login';
});

function route() {
  const hash = window.location.hash || '#/login';
  const [path, ...params] = hash.slice(1).split('/').filter(Boolean);

  document.getElementById('app').innerHTML = '';

  if (path === 'login' || path === 'register') {
    renderAuth(path);
  } else if (path === 'create') {
    renderTask('create');
  } else if (path === 'progress') {
    renderTask('progress', params[0]);  // taskId
  } else if (path === 'works') {
    renderWorks();
  } else {
    window.location.hash = '#/login';
  }
}

window.addEventListener('hashchange', route);
route();  // 初始渲染
```

---

## 7. 与 client-sdk-spec.md 接口对应

| SDK 接口（client-sdk-spec.md） | H5 实现函数 | 所在文件 |
|---|---|---|
| `AuthSDK.register` | `register(email, pw, pwConfirm)` | `api/auth.js` |
| `AuthSDK.login` | `login(email, pw)` | `api/auth.js` |
| `AuthSDK.refreshToken` | `tokenStore.refresh()` | `storage/tokenStore.js`（自动） |
| `AuthSDK.logout` | `logout()` | `api/auth.js` → `tokenStore.clear()` |
| `TaskSDK.createTask` | `createTask(params)` | `api/task.js` |
| `TaskSDK.getTaskStatus` | `getTask(taskId)` | `api/task.js` |
| `TaskSDK.watchTask` | `watchTask({ taskId, onUpdate, onError })` → `cancelFn` | `api/task.js` |
| `TaskSDK.cancelWatch` | 调用 `watchTask()` 返回的 `cancelFn()` | `api/task.js` |
| `WorkSDK.selectCandidate` | `selectCandidate(taskId, candidate)` | `api/work.js` |
| `WorkSDK.listWorks` | `listWorks(page)` | `api/work.js` |
| `WorkSDK.getDownloadURL` | `getDownloadUrl(workId)` | `api/work.js` |
| `CreditSDK.getCredits` | `getCredits()` | `api/credit.js` |
| `AudioPlayer.load` | `player.load(url)` | `audio/player.js` |
| `AudioPlayer.play` | `player.play()` | `audio/player.js` |
| `AudioPlayer.pause` | `player.pause()` | `audio/player.js` |
| `AudioPlayer.seek` | `player.seek(positionSeconds)` | `audio/player.js` |
| `AudioPlayer.setVolume` | `player.setVolume(value)` | `audio/player.js` |
| `AudioPlayer.release` | `player.release()` | `audio/player.js` |

---

## 8. 错误处理

参照 `client-sdk-spec.md §8` 错误码映射，在 `ui/components/errorToast.js` 集中展示：

| API 错误码 | H5 处理 |
|---|---|
| `UNAUTHORIZED` (401) | `client.js` 自动刷新；失败 → `calliope:sessionExpired` 事件 → 跳 `#/login` |
| `INSUFFICIENT_CREDITS` (402) | errorToast 提示"今日配额已用完" |
| `CONTENT_FILTERED` (400) | 输入框下方 inline 提示文字 |
| `QUEUE_FULL` (429) | errorToast 提示"服务繁忙，请稍后再试" |
| `RATE_LIMIT_EXCEEDED` (429) | 同上，附加 Retry-After 倒计时 |
| 5xx | errorToast 提示"服务器异常"，不清除本地状态 |
| 网络异常（fetch 抛出） | errorToast 提示"网络不可用"；watchTask WS 失败自动降级轮询 |
| Safari play() 被拒绝 | 显示"点击播放"按钮，等待用户手势 |
