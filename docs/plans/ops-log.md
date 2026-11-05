# 运营日志（代理交接）

本文件记录跨会话的现状、决定与待办。新会话先读这里，再读 `AGENTS.md` §10 / §10.7。

## 当前状态（2026-11-06 00:20）

| 项 | 值 |
| --- | --- |
| 生产版本 | `v1.7.3.14`；`v1.8.0` 已发布，等待后台升级 |
| 仓库 | `origin/main` = 本地 `main` = `6da13ea2`，tag `v1.8.0` |
| 工作区 | **只有 `E:\zm-ai-canvas` 一个**，`.git` 在这里 |
| 未完成功能 | `codex/wip-password-registration`（免验证码注册，勿合入 main） |
| 上游 | 已同步到作者 `v1.8.0`（33 个提交、458 个文件） |

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
2. `codex/wip-password-registration` 那支免验证码注册还没做完，也没验证过；要么继续，要么丢弃。
3. 若要真正的"挂机"：GitHub Actions 定时做上游预检与开 PR，而不是让代理点鼠标。

已完成：插件 1.0.5 已上传并在钱包页确认——品牌图标正常，文案为「使用支付宝支付 / 使用微信支付」。

## 变更记录

- 2026-11-05 23:29 插件 1.0.5 上线验证通过（钱包页图标与文案生效，插件页版本 1.0.5）。

## 约定

- 代理执行同步、验证、发版；生产动作（后台点更新、回滚）由用户确认。
- 汇报格式：做了什么 → 结果 → 下一步等什么确认，三行以内。
- 发版只用 `scripts/cut-release.mjs`，不手写 VERSION/CHANGELOG。
