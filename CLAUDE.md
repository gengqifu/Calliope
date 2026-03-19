# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Calliope 是一个模仿 Suno/Udio 的 AI 音乐生成系统，支持 Android（Kotlin）、iOS（Swift）、H5（原生 JS）三端原生开发。

**项目附加目标：** 项目发起人（Android 架构师）通过此项目成长为后端/系统/AI 架构师，技术选型优先考虑学习价值而非最短路径。

详细项目计划见 [docs/project-plan.md](docs/project-plan.md)，**开始新对话前请先阅读该文档**。

## 架构概览

系统分为两个后端服务：

```
Go API 服务（业务层）
├── 用户认证、任务管理、文件管理
├── 框架：Gin → 后期迁移 gRPC
└── 与推理服务通过 Redis 队列解耦

Python 推理服务（AI 层）
├── AudioCraft / MusicGen 音乐生成
├── 框架：FastAPI
└── Worker 消费 Redis 队列任务，结果写入 OSS
```

## 技术选型（已确认，勿随意更改）

| 层 | 选型 |
|--|--|
| API 层 | Go + Gin |
| AI 推理层 | Python + FastAPI + AudioCraft |
| 数据库 | MySQL |
| 缓存 / 任务队列 | Redis |
| 对象存储 | 阿里云 OSS（或 MinIO 自建） |
| GPU 推理服务器 | AutoDL 按需租用 |
| 容器化 | Docker（当前阶段），K8s（第三阶段引入） |

## 常用命令

### Go API 服务

```bash
go build ./...
go test ./...
go test ./path/to/package -run TestName   # 运行单个测试
go vet ./...
gofmt -w .
```

### Python 推理服务

```bash
pip install -r requirements.txt
uvicorn main:app --reload                 # 开发模式启动
pytest                                    # 运行测试
pytest tests/test_foo.py::test_bar        # 运行单个测试
```

### Docker

```bash
docker compose up -d                      # 本地联调启动全部服务
docker compose logs -f [service]          # 查看服务日志
```

## 开发原则

- **TDD 优先**：先写测试，再写实现
- **接口先行**：新模块先定义接口（OpenAPI），确认后再实现
- **Plan Mode**：每次实现新模块或执行部署操作前，使用 `/plan` 规划后再执行
- **ADR 记录**：重要架构决策记录在 `docs/adr/` 目录

## 代码目录结构

```
Calliope/
├── api/            # Go API 服务（含 api/CLAUDE.md）
├── inference/      # Python 推理服务（含 inference/CLAUDE.md）
├── android/        # Android App（含 android/CLAUDE.md）
├── ios/            # iOS App（含 ios/CLAUDE.md）
├── h5/             # H5 Web App（含 h5/CLAUDE.md）
├── docker/         # docker-compose 和 Dockerfile
└── docs/           # 所有设计文档和规范
```

**各子目录均有独立 CLAUDE.md**，包含该端的强制编码约束。进入对应目录工作时，Claude 会自动叠加读取。

## 关键文档

- [docs/project-plan.md](docs/project-plan.md) — 项目主计划，包含阶段任务和当前状态
- [docs/claude-best-practices.md](docs/claude-best-practices.md) — Claude 使用最佳实践
- [docs/architecture/](docs/architecture/) — 架构设计文档（阶段2产出）
- [docs/adr/](docs/adr/) — 架构决策记录
- [docs/coding-standards/go.md](docs/coding-standards/go.md) — Go 详细编码规范
- [docs/coding-standards/python.md](docs/coding-standards/python.md) — Python 详细编码规范
- [docs/coding-standards/android.md](docs/coding-standards/android.md) — Android 详细编码规范
- [docs/coding-standards/ios.md](docs/coding-standards/ios.md) — iOS 详细编码规范
- [docs/coding-standards/h5.md](docs/coding-standards/h5.md) — H5 详细编码规范
