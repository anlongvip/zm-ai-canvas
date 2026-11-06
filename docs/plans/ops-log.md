# 运营日志（代理交接）

本文件记录跨会话的现状、决定与待办。新会话先读这里，再读 `AGENTS.md` §10 / §10.7。

## 当前状态（2026-11-06 11:10）

| 项 | 值 |
| --- | --- |
| 生产版本 | **`v1.8.0`**；`v1.8.1` 已发布，等待后台升级（Schema 87→89） |
| 仓库 | `origin/main` = 本地 `main` = `a03bfc75`，tag `v1.8.1` |
| 工作区 | **只有 `E:\zm-ai-canvas` 一个**，`.git` 在这里 |
| 分支 | 只有 `main`（其余已清理） |
| 上游 | 已同步到作者 `v1.8.1`（21 个提交、68 个文件） |

## v1.8.1 发版记录（2026-11-06 10:06）

- 上游范围：`2fe3ca13..8611fc0e`，21 个提交、68 个文件，+2704/−165。
- 冲突只有 `CHANGELOG.md`；4 个计费文件（`finance.go`、`finance_billing.go`、`models_finance.go`、`drama_orchestrator.go`）自动合并。
- 迁移演练：87 → 89 通过，`migrate-schema verify` 返回 `ready=true`。
- 全量验证：第一次 `backend:test-rest` 失败（`TestCanvasAgentActivityIndexed/postgres/pi-history-indexes`
  报缺索引），单独跑该测试与整包均通过，重跑全量 12/12 通过 —— **判定为既有测试不稳定，与 1.8.1 无关**。
- CI：main 5m23s 通过；tag 轮成功（镜像复用生效）；Release `v1.8.1` 正式发布、4 附件齐全。
- 上游 1.8.1 要点：参考视频计费（默认关闭）、声明式插件晚到结果、合成失败原因、短剧参考图。
- **待用户操作**：后台升级到 `v1.8.1`，确认 Schema 89。

## 生产升级记录：v1.7.3.14 → v1.8.0（2026-11-06 00:21）

- 迁移：Schema **82 → 87**，升级器状态 `succeeded`，未触发回滚。
- 备份：`backup-20261105-162148`，431.76 MB，来源 `v1.7.3.14`。
- **保留策略首次生效**：升级日志出现「已清理旧备份：backup-20261104-113440.zip」。
  说明 v1.7.3.14 装的更新器已含 `retention.go`，无需再等一轮。
- 迁移确认：`select max(version) from schema_migrations` 返回 **87**；
  `docker compose run --rm migrate verify` 返回 `{"current":87,"expected":87,"ready":true}`。
- 启动日志无 panic / 5xx；v1.8.0 新增的灵感大厅同步已运行（`inspiration feed synced created=311`）。

## v1.8.0 合并记录（2026-11-06 00:00）

- 上游范围：`878b515b..2fe3ca13`，33 个提交、458 个文件；Schema **82 → 87**。
- 6 个冲突：`VERSION`、`CHANGELOG.md`、`agent-runtime.mjs`、`cloud_agent_pi_coordinator.go`、`cloud_agent_pi_model.go`、`drama.ts`。
- **真实回归**：上游新增的「步骤预算耗尽」拦截插在「认领孤儿任务」之前，会让中断恢复直接失败；
  由二开测试 `TestCloudAgentModelStepAdoptsOrphanedTask` 抓到，删除重复拦截后修复（`6da13ea2`）。
- 迁移演练：空库 0→87 通过；增量 82→87 通过（`migrate-schema verify` 返回 `ready=true`）。
- 前端新增依赖 `react-i18next` / `i18next`，需 `bun install`。
- Release 侧：`gh release create` 转正式时 GitHub 返回 500（瞬时故障），改为 API 重建 Release 并上传四个附件；
  附件哈希与本地构建一致。

## 下次升级要盯

v1.8.0 装好后保留策略才开始生效。再下一次升级应看到：
- 更新日志出现「已清理旧备份：…」；
- `df -h` 不再每版掉约 1.4GB。

若没有，检查 `CANVAS_UPDATER_BACKUP_RETENTION` 与备份目录权限。

## 已知坑（都踩过，别再踩）

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| 验证报 `UNIQUE constraint failed: user_oss_settings.id` | 测试第 66 行**故意**制造重复插入 | 噪音，不是失败 |
| `backend:test-rest` 偶发失败（索引断言） | 多包并发共用同一 Postgres 测试库 | 已加 `-p 1`；若 CI 红了先重跑确认 |
| PowerShell 跑 `*.ps1` 报 ParserError | 脚本是无 BOM 的 UTF-8，PS 5.1 按 ANSI 读 | 存成带 BOM 的 UTF-8（`scripts/verify-linux.ps1` 已是 BOM） |
| `pack.py` 报 `Unexpected UTF-8 BOM` | 同上，反向问题 | JSON 写无 BOM 的 UTF-8 |
| 插件渠道图标显示灰底 PAY | 清单 icon 写的是包内路径，宿主只认 `brand:*` | manifest 用品牌键 |

## 待办（都不紧急）

1. 上游那批更新推完后：merge → `scripts/verify-linux.ps1` → `cut-release` → 后台更新。
2. 若要真正的"挂机"：GitHub Actions 定时做上游预检与开 PR，而不是让代理点鼠标。

已完成：
- 插件 1.0.5 已上传并在钱包页确认——品牌图标正常，文案为「使用支付宝支付 / 使用微信支付」。
- ~~免验证码注册无需二开~~ **此条曾误记**：上游 v1.8.0/v1.8.1 并未实现 `PasswordRegistration`，
  免验证码注册一直是本地二开。上游 v1.8.2 才以 `EmailRequired` / `EmailVerificationRequired`
  两个开关正式实现等价能力（可关闭注册验证码、可要求必填邮箱），并重构了注册表单。
  本次同步 v1.8.2 时已删除本地二开，改为完全使用上游实现。
- **分支收敛完毕**：仓库只剩 `main`。`codex/runninghub-video-phase1` 的目录特判已在 main，
  `codex/runninghub-phase1-complete` 的只读审计 SQL 已收编为 `scripts/audit-runninghub-billing.sql`，
  其余过期文档未保留。

## 变更记录

- 2026-11-05 23:29 插件 1.0.5 上线验证通过（钱包页图标与文案生效，插件页版本 1.0.5）。
- 2026-11-06 00:21 生产升级 v1.8.0 成功，Schema 87 确认；保留策略首次清理旧备份。
- 2026-11-06 00:45 清理分支：删除已合并的 8 个临时/同步分支，仅保留 main 与两支早期 runninghub 分支。
- 2026-11-06 01:00 仓库只剩 `main`；`scripts/verify-linux.ps1` 增加旧验证副本清理（只保留最近一份历史副本）。

## 环境说明（不要误删）

- `.local/wsl/zm-ai-canvas-ci`（约 21 GB）是 Linux 验证环境的虚拟磁盘，`scripts/verify-linux.ps1` 依赖它。
  用户在用它，**不要为了省空间删除**；如需瘦身，先清 `/opt/canvas-ci/verify/*` 与 apt 缓存。
- `.local/` 整体被 git 忽略，里面是缓存、日志与验证产物；清理时只删 `cache/` 下的可重建内容。

## 约定

- 代理执行同步、验证、发版；生产动作（后台点更新、回滚）由用户确认。
- 汇报格式：做了什么 → 结果 → 下一步等什么确认，三行以内。
- 发版只用 `scripts/cut-release.mjs`，不手写 VERSION/CHANGELOG。
