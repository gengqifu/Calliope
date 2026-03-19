# Go 编码规范

> 适用范围：`api/` 目录下所有 Go 代码

---

## 工具链

| 工具 | 版本 | 用途 |
|--|--|--|
| Go | 1.22+ | 语言运行时 |
| gofmt | 内置 | 代码格式化（强制） |
| go vet | 内置 | 静态分析 |
| golangci-lint | 1.57+ | 聚合 lint（CI 必跑） |

### golangci-lint 启用的检查器

```yaml
# .golangci.yml
linters:
  enable:
    - errcheck      # 不允许忽略 error 返回值
    - gocritic      # 代码质量建议
    - goimports     # import 分组格式
    - misspell      # 英文拼写检查
    - revive        # golint 替代品
    - staticcheck   # 静态分析
```

---

## 目录结构规范

```
api/
├── cmd/
│   └── server/
│       └── main.go         # 程序入口，只做初始化和启动
├── internal/
│   ├── handler/            # HTTP handler，只负责解析请求和返回响应
│   ├── service/            # 业务逻辑层（interface + impl）
│   ├── repository/         # 数据访问层（interface + impl）
│   ├── model/              # 数据库模型（GORM struct）
│   ├── dto/                # 请求/响应 DTO（与 model 分离）
│   ├── middleware/         # Gin 中间件
│   └── infra/              # 基础设施初始化（DB、Redis、OSS）
├── pkg/                    # 可对外复用的工具包
│   ├── errors/             # 统一错误类型
│   ├── logger/             # 日志封装
│   └── validator/          # 参数校验工具
├── config/                 # 配置结构体和加载逻辑
├── migrations/             # SQL migration 文件
├── go.mod
└── go.sum
```

---

## 命名规范

### 包名

- 全小写，单个单词（`handler`、`service`、`repository`）
- 禁止下划线或驼峰（`user_service` ❌，`userService` ❌）

### 接口与实现

```go
// 接口：动词/能力命名，放 service/ 目录
type TaskCreator interface {
    CreateTask(ctx context.Context, req dto.CreateTaskRequest) (*dto.Task, error)
}

// 实现：struct 名带 Impl 后缀
type taskServiceImpl struct {
    repo repository.TaskRepository
}
```

### 变量与函数

```go
// 导出：PascalCase
func CreateTask(...) {}

// 不导出：camelCase
func validatePrompt(...) {}

// 常量：PascalCase（导出）或 camelCase（内部）
const MaxPromptLength = 500
const defaultTimeout = 30 * time.Second
```

---

## 错误处理规范

### 包裹错误（必须）

```go
// ✅ 正确：提供上下文，保留原始错误链
if err != nil {
    return fmt.Errorf("taskService.CreateTask: %w", err)
}

// ❌ 错误：丢失调用栈上下文
if err != nil {
    return err
}

// ❌ 错误：裸 errors.New（无法包裹原始错误）
return errors.New("create task failed")
```

### 业务错误类型

```go
// pkg/errors/errors.go
type AppError struct {
    Code    string // 对外错误码，如 "TASK_NOT_FOUND"
    Message string // 对外错误信息
    Err     error  // 原始错误（不对外暴露）
}

func (e *AppError) Error() string { return e.Message }
func (e *AppError) Unwrap() error { return e.Err }

// 预定义错误
var ErrTaskNotFound = &AppError{Code: "TASK_NOT_FOUND", Message: "task not found"}
```

### Handler 层统一返回

```go
// ❌ 禁止在 handler 层直接暴露内部错误
c.JSON(500, gin.H{"error": err.Error()})

// ✅ 通过中间件统一处理
c.Error(err)  // 挂载错误，由 error middleware 统一格式化返回
```

---

## 分层约束

| 层 | 可依赖 | 禁止依赖 |
|--|--|--|
| handler | service（interface）、dto | repository、model、infra |
| service | repository（interface）、model | handler、infra 直接调用 |
| repository | model、infra（DB/Redis） | service、handler |
| infra | 外部库 | 任何业务层 |

---

## 测试规范

### 表驱动测试（必须）

```go
func TestCreateTask(t *testing.T) {
    tests := []struct {
        name    string
        req     dto.CreateTaskRequest
        wantErr bool
    }{
        {"valid request", dto.CreateTaskRequest{Prompt: "jazz music"}, false},
        {"empty prompt", dto.CreateTaskRequest{Prompt: ""}, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ...
        })
    }
}
```

### Mock 规范

- 使用 `github.com/stretchr/testify/mock` 或 `go.uber.org/mock`
- Mock 只在单元测试中使用；集成测试连接真实 DB/Redis（Docker）

### 覆盖率要求

- 整体 > 70%
- `service/` 层 100%
- `handler/` 层 > 80%（测试 HTTP 状态码和响应格式）

---

## 并发规范

```go
// ✅ 使用 context 控制超时和取消
ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()

// ✅ goroutine 必须有退出机制
go func() {
    select {
    case <-ctx.Done():
        return
    case task := <-taskCh:
        process(task)
    }
}()
```

---

## import 分组顺序

```go
import (
    // 1. 标准库
    "context"
    "fmt"

    // 2. 第三方库
    "github.com/gin-gonic/gin"
    "go.uber.org/zap"

    // 3. 本项目内部包
    "github.com/yourorg/calliope/internal/dto"
)
```

用 `goimports` 自动维护，禁止手动乱序。
