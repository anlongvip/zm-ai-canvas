# 运营日志（代理交接）

本文件记录跨会话的现状、决定与待办。新会话先读这里，再读 `AGENTS.md` §10 / §10.7。

## 当前状态（2026-11-07 08:00）

| 项 | 值 |
| --- | --- |
| 生产版本 | **`v1.8.2`**，Schema **91**；五服务 healthy，镜像已固定 digest |
| 仓库 | 本地 `main` = `2a71c2b8`，tag `v1.8.2` 已发布（Release #437201502） |
| 工作区 | **只有 `E:\zm-ai-canvas` 一个**，`.git` 在这里 |
| 分支 | 只有 `main`（其余已清理） |
| 上游 | 已同步到作者 `v1.8.2`（`8611fc0e..cd2512eb`，18 个提交、219 个文件） |
| 生产域名 | `https://ai.zhimomedia.com`（宝塔反代 → 127.0.0.1:3000），`zhimo.pro` 301 跳转 |

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
| `verify-linux.ps1` 大批用例失败（app/rest/canvas/database） | WSL 里 PostgreSQL 与 Redis 没起，WSL 无 systemd | `wsl -d zm-ai-canvas-ci -- bash -lc "service postgresql start; service redis-server start"`，再跑验证 |
| 本机 `gofmt -l` 报全仓库文件未格式化 | Windows 检出 `core.autocrlf=true`，文件是 CRLF | 本地不看 gofmt 结论，以 WSL/Linux 验证为准 |
| `git fetch upstream` 报 `schannel SEC_E_NO_CREDENTIALS` | 本机 Git 走 schannel 取不到 TLS 凭证 | `git config --local http.sslBackend openssl` |
| 后台点「开始更新」在备份步骤失败 | 服务器 Compose `v2.28.1` 不支持 `compose run --pull never`（`manager_ops.go` 备份/恢复依赖它） | 升级 Compose CLI 插件到 `v2.40.3`，Docker Engine 不用换 |

## 待办（都不紧急）

1. **3000 端口暂不关闭**（用户 2026-11-07 决定，先开着方便调试）。风险已知：走 IP:3000 是 HTTP 明文，
   且绕过 HTTPS。等有真实用户必须关：云安全组删除 3000 入站，并把 `.env` 的 `CANVAS_HTTP_PORT`
   改为 `127.0.0.1:3000` 后 `--force-recreate web`（宝塔反代本来就走本机，不受影响）。
   注意：只删宝塔防火墙规则无效，Docker 发布端口会绕过主机防火墙。
2. 注册验证开关待定：未配 SMTP 时应在后台关闭「注册需要邮箱验证」，否则新用户注册收不到验证码。
3. 对象存储已配置并测试连接通过（阿里云），待确认保存与真实上传；通过后按
   `docs/plans/pricing-and-storage-tiers.md` 上架存储扩容商品（在此之前不得售卖存储容量）。
4. 若要真正的"挂机"：GitHub Actions 定时做上游预检与开 PR，而不是让代理点鼠标。

已完成：
- 生产 `v1.8.2` 升级后登录与画布加载已验证（2026-11-07 用户截图确认）；画布写入路径尚未单独复验。
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
- 2026-11-07 08:00 合并上游 v1.8.2（`8611fc0e..cd2512eb`，18 提交/219 文件，Schema 89→91）。
  处理 7 处冲突：认证类全部采用上游版本并删除本地免验证码注册二开；渠道投影保留二开并叠加上游媒体传输装饰。
  首次验证 5 项失败原因为 WSL 数据库服务未启动，启动 PostgreSQL/Redis 后 12 项全过（232 秒）。
- 2026-11-07 08:13 `v1.8.2` 发布完成：main CI 6m19s 全绿（三镜像已构建），tag 轮 2m35s、质量检查复用 main 结果跳过、
  Release 四附件齐全（compose、updater amd64/arm64、SHA256SUMS）。生产仍是 `v1.8.1`/Schema 89，待后台升级到 Schema 91。
- 2026-11-07 08:24 生产升级 `v1.8.1 → v1.8.2` 成功。升级前把服务器 Compose 从 v2.28.1 升到 **v2.40.3**
  （备份步骤依赖 `compose run --pull never`，低于 2.30 会失败）。备份 `backup-20261107-002414`，411.63 MB，
  来源 v1.8.1。升级后 Schema **91**，五服务 healthy，backend 日志无 panic/error。
  三镜像 digest：backend `sha256:18ab27e4737719f5b32077e8c06cc47d3a917f5c418a5d626cbe0f89988a99d4`、
  web `sha256:3946ac8c328b0ee453c245add696d18692e62db3c0b5739ecf103fbc89eb6dd8`、
  yingce-agent `sha256:7114dd88483897094ed5c4b0e4cf1b037f9817949aca35db92a1b9d65aa70064`。
  未验证：登录与画布实操抽查（待用户确认）。

## 环境说明（不要误删）

- `.local/wsl/zm-ai-canvas-ci`（约 21 GB）是 Linux 验证环境的虚拟磁盘，`scripts/verify-linux.ps1` 依赖它。
  用户在用它，**不要为了省空间删除**；如需瘦身，先清 `/opt/canvas-ci/verify/*` 与 apt 缓存。
- `.local/` 整体被 git 忽略，里面是缓存、日志与验证产物；清理时只删 `cache/` 下的可重建内容。

## 约定

- 代理执行同步、验证、发版；生产动作（后台点更新、回滚）由用户确认。
- 汇报格式：做了什么 → 结果 → 下一步等什么确认，三行以内。
- 发版只用 `scripts/cut-release.mjs`，不手写 VERSION/CHANGELOG。
