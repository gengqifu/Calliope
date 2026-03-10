# ADR-001: API 层使用 Go 而非 Python 或 Node.js

- 日期：2026-03-09
- 状态：已确认

---

## 背景

Calliope 系统需要一个 API 服务层处理：用户认证、任务管理、WebSocket 实时通知、文件 URL 签名等业务逻辑。选择哪种语言/框架直接影响开发效率、运行时性能和团队成长路径。

项目发起人是 Android 架构师，有 Java/Kotlin 背景，无 Go/Python/Node.js 生产经验。项目的附加目标是"通过此项目成长为后端/系统/AI 架构师"，因此技术选型需要兼顾**学习价值**和**工程实用性**。

---

## 决策

**API 层使用 Go + Gin 框架。**

---

## 备选方案

| 方案 | 优点 | 缺点 |
|------|------|------|
| **Python + FastAPI** | 与 AI 推理层同一语言，学习曲线低 | FastAPI 基于 asyncio，I/O 并发能力与 goroutine 相当；但 async/await 传染性强，调试 stacktrace 更难读；GIL 对 CPU 密集型仍是瓶颈 |
| **Node.js + Express/Fastify** | 事件循环适合 I/O 密集；生态成熟 | 动态类型易出运行时错误；不利于系统层成长 |
| **Go + Gin** | 静态类型、编译期发现错误；goroutine 天然适合 WebSocket 高并发；二进制部署简单；行业认可度高 | 需要学习新语言；错误处理冗余（无异常机制） |

---

## 理由

1. **并发模型适配**：Calliope 需要维持 WebSocket 长连接。Go 的 goroutine（~2KB 栈，调度器自动管理）相比 FastAPI 的 async/await（协程传染性强、调试 stacktrace 碎片化）在认知负担和调试体验上更直观。MVP 目标是 50 人同时在线，goroutine 模型远超此需求；千级并发是扩展余量，不是 MVP 约束。

2. **学习价值最高**：Go 是后端/系统方向的核心语言（Google、字节、哔哩哔哩等大量使用），静态类型 + 接口设计模式与 Java/Kotlin 的思维迁移成本低，且能直接学习到工业级后端开发范式。

3. **工程质量**：静态类型在编译期发现大量错误；标准库（net/http、sync、context）设计优雅，减少对第三方依赖的依赖；gofmt 强制统一代码风格。

4. **部署简单**：编译为单二进制，无运行时依赖，Docker 镜像可以极小（scratch 基础镜像 < 20MB），与 Python 的依赖管理地狱相比运维负担低。

5. **与 AI 层解耦合理**：AI 推理层必须用 Python（AudioCraft 生态），API 层用 Go 通过 Redis Stream 解耦，两层技术栈各司其职，不会产生语言混用的维护困扰。

---

## 后果

**优点：**
- 高并发 WebSocket 处理无压力
- 编译期类型安全减少线上 bug
- 学习价值高，成长路径清晰
- 二进制部署，运维简单

**缺点/风险：**
- 需要学习 Go 语言基础（预计 1-2 周上手，1-2 个月熟练）
- Go 错误处理冗余（`if err != nil`），初期代码可读性稍差
- 相比 Python/Node.js 的 CRUD 脚手架，Go 需要手写更多模板代码

**缓解措施：**
- 使用 Gin 框架减少样板代码
- 参考 Go 社区成熟的项目结构规范（golang-standards/project-layout）
- 通过 Claude Code 辅助生成重复性代码

---

## 复审触发条件

出现以下情况时复审本决策：

| 触发条件 | 建议动作 |
|---------|---------|
| 服务间调用超过 3 个微服务，REST 序列化开销成为瓶颈 | 评估 Gin → gRPC 迁移（project-plan.md 技术选型已注明为后期计划，阶段待定） |
| 团队扩展到 3 人以上且均有 Python 背景 | 重新评估 FastAPI 方案的维护成本 |
| Go API 成为 AI 功能迭代瓶颈（频繁跨语言协调） | 考虑将部分业务逻辑下移至 Python Worker |
