# 运营日志（代理交接）

本文件记录跨会话的现状、决定与待办。新会话先读这里，再读 `AGENTS.md` §10 / §10.7。

## 当前状态（2026-11-05 23:20）

| 项 | 值 |
| --- | --- |
| 生产版本 | `v1.7.3.14`，无回滚，五个服务健康，磁盘 39% |
| 仓库 | `origin/main` = 本地 `main` = 含 §10.7 的提交 |
| 发版工作区 | `E:\canvas-release`（干净 `main` 检出） |
| 主工作区 | `E:\zm-ai-canvas`（旧分支，有用户未提交改动，**不要动**） |
| 上游 | 领先 origin/main 13 个提交（截至 23:00），作者预告今晚还有约 20 个提交 |

## 今晚完成（v1.7.3.13 → v1.7.3.14）

1. 更新器新增备份保留（默认 3 份）与旧镜像回收，环境变量 `CANVAS_UPDATER_BACKUP_RETENTION` / `CANVAS_UPDATER_PRUNE_IMAGES`。
2. 更新器选版过滤 draft 与 prerelease。
3. CI tag 轮复用同提交镜像：发版从「main 6m37s + tag 10min+」降到「6m37s + 2m58s」。
4. 新增 `scripts/cut-release.mjs`：一次完成 VERSION/CHANGELOG/tag，已备好版本时只补 tag。
5. 本地验证 `backend:test-rest` 加 `-p 1`，修掉共享 Postgres 下的假失败。
6. 合入 GPT 的计费修复（`2beba18c`）与验证脚本 Windows 兼容修复（`632ecbf8`）。
7. 支付插件 1.0.5：渠道名改「使用支付宝支付 / 使用微信支付」，图标改宿主品牌键 `brand:alipay` / `brand:wechat-pay`。

## 下次升级要盯

v1.7.3.14 首次把**新更新器**装到服务器，所以保留策略要到**下一次升级**才生效。下次升级后应看到：
- 更新日志出现「已清理旧备份：…」；
- `df -h` 不再每版掉约 1.4GB。

若没有，检查 `CANVAS_UPDATER_BACKUP_RETENTION` 与备份目录权限。

## 已知坑（都踩过，别再踩）

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| 验证报 `UNIQUE constraint failed: user_oss_settings.id` | 测试第 66 行**故意**制造重复插入 | 噪音，不是失败 |
| `backend:test-rest` 偶发失败（索引断言） | 多包并发共用同一 Postgres 测试库 | 已加 `-p 1`；若 CI 红了先重跑确认 |
| PowerShell 跑 `.local/*.ps1` 报 ParserError | 脚本是无 BOM 的 UTF-8，PS 5.1 按 ANSI 读 | 存成带 BOM 的 UTF-8 |
| `pack.py` 报 `Unexpected UTF-8 BOM` | 同上，反向问题 | JSON 要无 BOM |
| `E:\zm-ai-canvas` 的 AGENTS.md 是旧版 | 该工作区停在旧分支 | 新会话一律用 `E:\canvas-release` |
| 插件渠道图标显示灰底 PAY | 清单 icon 写的是包内路径，宿主只认 `brand:*` | manifest 用品牌键 |

## 待办（都不紧急）

1. 上游那批更新推完后：merge → `bun scripts/verify-before-push.mjs --jobs 4` → `cut-release` → 后台更新。
2. 把 `.local/verify-linux.ps1` 的改造搬进 `scripts/verify-linux.ps1`（`.local/` 不进 git，换 worktree 就丢）。
3. 清理旧 Codex worktree：`8d78`、`release-v1.7.3.10`；`release-v1-7-3-12` 先留着（存有上面那份 `.local` 脚本）。
4. 主工作区那 9 个未提交改动的处置（提交到分支或丢弃），决定后可以把主工作区切到 `main`、删掉 `E:\canvas-release`。
5. 若要真正的"挂机"：GitHub Actions 定时做上游预检与开 PR，而不是让代理点鼠标。

## 约定

- 代理执行同步、验证、发版；生产动作（后台点更新、回滚）由用户确认。
- 汇报格式：做了什么 → 结果 → 下一步等什么确认，三行以内。
- 发版只用 `scripts/cut-release.mjs`，不手写 VERSION/CHANGELOG。
