# Docker 分支裁剪 + Portkey 风格请求日志方案

- **日期**：2026-07-15
- **基线**：fork 版本 `v2.9.37`
- **决策记录**：
  - 分支名：`feat/docker`（去掉 "only" 后缀）
  - `desktop/` 在新分支上**彻底删除**
  - 一并实现 Portkey 风格原始请求日志（方案 B：内存 + 流式重拼）

---

## 一、Docker 构建链路核实

**Docker 构建本就不含桌面端**，已亲自核实：

- `Dockerfile` 只 `COPY` `Makefile/scripts/frontend/backend-go/shared/VERSION`，**根本没有 `COPY desktop/`**。
- `Makefile` 的 `build` target 只调 `cd backend-go && $(MAKE) build`（行 54-55），不涉 desktop。
- `desktop/` 是独立的 **Wails 3 桌面壳**：
  - `desktop/main.go` 通过 `internal/backend` 把 `ccx-go` 当子进程拉起
  - 自己另带一套 Vue 前端（`desktop/frontend/`）
  - 系统托盘、单实例锁、updater、窗口状态持久化

**结论**：新建 docker 分支几乎无需 `backend-go` 改动，主要是机械裁剪桌面壳相关目录 + 同步 `Makefile` / `README` / CI 流水线。

## 二、代码体量（git 跟踪的源文件数）

| 模块 | 文件数 | Docker 端是否需要 |
|---|---|---|
| `backend-go/` | 375 个 Go | **必要** |
| `frontend/` | 206 个 ts/vue | **必要**（嵌入到后端） |
| `desktop/` | 45 个 Go + 351 个 ts/vue | **不必要** |
| `packaging/homebrew` | - | **不必要**（桌面/GitHub Releases 用） |
| `scripts/` | 5 个脚本 | 部分必要（`install-wails3.sh` 不必要） |
| `shared/` | 413 条 | **必要**（channel-presets / model-registry） |

## 三、Docker 端功能精简建议

| 模块 | Docker 端必要性 | 精简建议 |
|---|---|---|
| `/desktop`（Wails 壳 + 桌面前端 + updater + 单实例锁 + 系统托盘） | **不必要** | 整目录删除 |
| `internal/copilot`（GitHub Copilot OAuth） | 看你定位 | 若不做 Copilot 中转可删；否则保留（#245 是它）。本期保留 |
| `internal/thinkingcache`（SQLite 持久 Claude thinking 缓存） | 看用量 | 重 IO 请求场景保留；本期保留 |
| `internal/conversation`（会话/手动序列覆盖） | 必要 | 保留 |
| `internal/scheduler`（多渠道路由/熔断/恢复） | 必要 | 保留；建议后续修 #256/#265 |
| `capability-test`（messages/chat/responses/gemini 四类） | 看运维 | 保留（无运行时开销，仅在管理 API 触发） |
| 全局统计 / cost 估算 / 模型 registry | 必要 | 保留 |
| `dist/`、`frontend/dist`、`backend-go/frontend/dist` | 必要 | 是构建产物，不删 |
| `packaging/homebrew` | 不必要 | 新分支删除 |
| `.github/workflows` 中桌面/MSIX/updater 流水线 | 不必要 | 新分支删除桌面部分 |
| `docker-compose.watchtower.yml` | 看部署 | 保留便于滚动更新 |
| `scripts/install-wails3.sh` | 不必要 | 删除 |
| `scripts/embed-frontend.sh` | 必要 | 保留（Docker 构建用） |
| `refs/`（外部参考） | 不必要 | 已在 `.claudeignore`，不跟踪则无需动 |

## 四、Portkey 风格"含原始请求的日志"现状

**已读源码坐实**：

- `ChannelLog`（`internal/metrics/channel_log.go:10`）**纯元数据**，无 request body / response body 字段；纯内存环形，每 metricsKey 上限 50 条。
- SQLite `request_records` 表（`sqlite_store.go:116`）只存 token 计数 + model + failure_class，**无 body 列**。
- `LogUpstreamResponse`（`common/request.go:224`）已实现但只写**日志文件**，受 `EnableResponseLogs && IsDevelopment()` 双开关，**不进 ChannelLog 不进 DB**。multipart/vectors 已脱敏；Authorization 头当前未显式脱敏（这是一个安全隐患建议顺便修）。

**前端展示现状**：

- `ChannelLogsDialog.vue` 展示的字段都是 `ChannelLogEntry`（`frontend/src/services/api-types.ts:581-617`），与后端字段一一对应 —— **无 requestBody / responseBody 字段**。
- `copyLogEntry` 也只复制这些元数据。

## 五、改造工作量评估（基于实际代码、按 ccx 既有风格估算）

| 方案 | 后端文件/改动 | 前端改动 | 代码量 | 风险点 |
|---|---|---|---|---|
| A. 最小可用：内存版（非流式 body） | `channel_log.go` + `channel_log_helper.go` + 各 handler `CompleteLog` 注入 body + 新增 `ENABLE_REQUEST_BODY_LOGS` 开关 | `api-types.ts` + `ChannelLogsDialog.vue` 加折叠 body 区 | **2~3 文件、~250 行** | 50 条 × 大 body 内存压力；流式 body 缺失 |
| B. 推荐：内存版 + 流式 SSE 重拼 | 上面 + `common/stream.go` 复用 `LimitedLogBuffer` 拼装完整 SSE 为 ResponseBody | 同 A | **4~5 文件、~450 行** | SSE 拼接复杂度、脱敏覆盖 |
| **C. 完整版**：B + SQLite 持久化 | B + `sqlite_store.go` 新建 `request_log_bodies` 表 + migration v3→v4 + 单条详情接口 `GET /api/{type}/channels/:id/logs/:requestId/body` + 删渠道级联清理 + Authorization/x-api-key 头脱敏 | B + 详情对话框 + 虚拟滚动 + JSON 美化 + 复制按钮 | **6~8 后端 + 3~4 前端、~1000~1400 行** | schema 迁移幂等、磁盘膨胀、清理策略 |

**选定方案**：**B**（内存 + 流式重拼）。理由：

- 50 条 × 限 1MB 已足够排查体验，不引永久磁盘膨胀风险。
- 不需要 schema 迁移，无 production 磁盘膨胀风险。
- 工作量适中，~450 行 / 2-3 天。
- 如确需长期归档再升级到 C。

## 六、执行计划

### 阶段 1 — 落地审查报告（文档）

- `docs/review/2026-07-14-code-review.md`（整体代码审查报告）
- `docs/review/2026-07-14-docker-branch-plan.md`（本文档）

### 阶段 2 — 新建分支并裁剪桌面端

1. `git checkout -b feat/docker`
2. 彻底删除 `desktop/`（`git rm -r desktop`）
3. 删除 `packaging/homebrew/`
4. 删除 `scripts/install-wails3.sh`
5. 编辑 `Makefile`：去掉 `desktop-dev/desktop-build/install` 三处 target 中 wails 桌面段，保留 `dev/run/build/frontend-*/clean/embed-frontend`
6. 编辑 `scripts/embed-frontend.sh`：若有桌面分支一并裁掉
7. 编辑 `README.md` / `README.zh-CN.md`：删除 CCX Desktop 桌面安装段，保留 Docker / 二进制段
8. 删除 `.github/workflows` 中仅桌面/MSIX/updater 相关流水线（保留 release/docker 流水线）

### 阶段 3 — 实施方案 B：Portkey 风格原始请求/响应日志（内存+流式重拼）

**backend-go**：

- `internal/metrics/channel_log.go`：`ChannelLog` 增 `RequestBody string` / `ResponseBody string` 字段（带大小限制常量 `MaxUpstreamResponseLogBytes = 1 << 20` 即 1MB）
- `internal/handlers/common/channel_log_helper.go`：`CreatePendingLog` / `CompleteLog` / `RecordChannelLogWithSource` 接收并注入 body；multipart/vectors 维持 omitted
- `internal/handlers/common/stream.go`：复用 `LimitedLogBuffer` 把流式 SSE 重拼为完整 `ResponseBody`
- `internal/config/env.go`：新增 `ENABLE_REQUEST_BODY_LOGS` / `ENABLE_RESPONSE_BODY_LOGS` 开关（默认 false，对齐生产安全）
- 各 handler `CompleteLog` 时机注入 body
- Authorization/x-api-key 头显式脱敏后入库（顺便修 `LogUpstreamResponse` 的响应头脱敏）
- 增加 `_test.go` 覆盖脱敏与流式重拼

**frontend**：

- `services/api-types.ts`：`ChannelLogEntry` 增 `requestBody?: string` / `responseBody?: string`
- `components/ChannelLogsDialog.vue`：详情展开区新增两个折叠的只读代码块 + JSON 美化 + 复制按钮（复用现有 `copyLogEntry` 模式）
- `plugins/vuetify.ts`：如新增 `VCode` / `VExpansionPanels` 在此注册

### 阶段 4 — 验证（不提交、不推送）

- `make build`
- `cd backend-go && make test`
- `cd frontend && bun run build`
- `git status` + `git diff --stat` 交给你 review 后再决定是否提交

## 七、风险与注意事项

| 风险 | 缓解 |
|---|---|
| 50 条 × 大 body 内存压力 | 限 `MaxUpstreamResponseLogBytes = 1MB`；超限截断并标记 `[truncated]` |
| SSE 拼接复杂度（多 chunk / 中断） | 复用现有 `LimitedLogBuffer`，超限即停止收集 |
| 脱敏覆盖 | Authorization / x-api-key 头显式 mask；multipart / vectors body 保持 `[omitted]` |
| upstream 合并冲突 | `feat/docker` 不再保留桌面端，合并 upstream 桌面更新会有冲突，需要手动取舍 |
| schema 迁移（方案 C 才有） | 本期不涉及；如升级到 C，参考既有 `ALTER ADD COLUMN` + `duplicate column name` 容错模式 |

## 八、与整体审查报告的关联

本方案与 [`2026-07-14-code-review.md`](2026-07-14-code-review.md) 协同：

- **第五章 P0-P3 优先级表**中的 P0（#265 URL 重复追加、#274 负值核实）不在本方案内，建议作为后续独立 PR。
- **#256 熔断自动恢复**待你决策，不在本方案内。
- **#275 / #258 / #269 Codex 工具 chat 兼容**待排期，不在本方案内。
- 本方案聚焦：**docker 分支裁剪 + Portkey 风格日志**，作为定制化的首个 PR 落地。