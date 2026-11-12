# 官方更新同步流程

本仓库使用两个远程：

- `origin`：`anlongvip/zm-ai-canvas`，保存二开代码、集成分支和发布版本。
- `upstream`：`ddcat-ai/open-ai-canvas`，只用于读取作者更新。

`origin/main` 保存已经合入的二开功能。每次从它创建集成分支，再合并作者提交；不要用作者分支覆盖自己的 `main`，也不要直接部署作者镜像替换二开镜像。

## 同步步骤

先确认工作区干净并记录当前发布版本、源提交和目标上游提交：

```bash
git status --short
git fetch --multiple origin upstream
git log -1 --oneline origin/main
git log -1 --oneline upstream/main
git switch -c codex/sync-upstream origin/main
git merge --no-edit upstream/main
```

分支名每次使用新的名称。如果合并冲突，逐处核对双方业务语义；没有 Git 冲突也不代表功能兼容。RunningHub Provider、档位配置、实际运行时间结算、取消退款、任务恢复、余额刷新及插件包属于二开范围，必须核对入口、调用方、接口字段和测试。实现范围及真实验收限制见 [RunningHub 第一阶段](plans/runninghub-video-phase1.md)。

不要仅用版本号比较作者版本与二开版本；以提交关系和实际差异为准。PR #8 从二开 `v1.7.3.8` 基线整合作者提交 `dc035d51`；后续同步同样保留二开自己的版本序列。

## 推送前验证

合并后的源码、锁文件与测试需要一起核对。迭代时先跑相关专项测试；最后提交全部改动、fetch 并整合最新远端，保持工作区干净，再执行默认范围检查：

```bash
bun scripts/verify-before-push.mjs --jobs 4
```

Windows 本机使用 [Linux 验证环境](plans/local-ci-environment.md) 的 `.local/verify-linux.ps1` 入口，避免换行和路径差异。该环境具备 PostgreSQL、Redis、Pi Agent 和 Chrome；不得通过跳过失败项换取推送。

检查通过后推送集成分支，向自己的 `origin/main` 创建 PR，确认 GitHub Actions 针对最终提交 SHA 全部通过，包括本地未覆盖的支付插件双架构构建。旧提交的通过结果不能证明新提交可发布。

## 发布与线上数据

PR 通过和合入不等于生产已更新。发布使用自己仓库构建的新版本；在新镜像、更新器和校验文件齐备后，按现有 Host Updater 流程升级。发布前确认数据库备份和旧镜像可回滚，保留数据库、Redis、Agent、OSS、配置和数据卷。

本轮同步以 `origin/main` 的 `v1.8.4.1`（`795ba1bb`）为基线，合并作者 `c8d274f4..ac5e01ca`（作者 v1.8.5）共 16 个提交、199 个文件，Schema 由 98 升至 100。合并冲突 2 处（`CHANGELOG.md`、`scripts/verify-before-push.mjs`）：后者取上游新增的 `backend:pi-runtime`（`node --test`）并保留二开「只对改动 `.go` 文件 gofmt」以避开 Windows CRLF 误报。本轮上游未改动 `finance|billing|refund|payment|hostupdate` 文件，二开计费规则路径不变。发布前注意：`git fetch upstream --tags` 会把作者 tag `v1.8.4` / `v1.8.5` 拉进本地且指向作者提交，打自己的 tag 前必须删除，否则 `cut-release.mjs` 的 `lastTag()` 会误取作者 tag。另：验证环境共用 `/opt/canvas-ci/repo/web/node_modules`，上游新增前端依赖后需把最新 `package.json`/`bun.lock` 覆盖过去并 `bun install`，否则测试会报缺包（本轮 `papaparse`）。

上一轮：以 `origin/main` 的 `v1.8.3`（`94023796`）为基线，合并作者 `ee6c0ee2..c8d274f4`（作者 v1.8.4 与 v1.8.4.1）共 65 个提交、923 个文件，Schema 由 92 升至 98；合并冲突 2 处（Agent 画布事件测试的注释、模型能力保存路径），`model_capability.go` 按上游「已保存能力由管理员决定、型号建议只构造默认配置」处理并保留 RunningHub 档位校验。

禁止把 `.env`、API Key、数据库、上传文件和 `.settings-key` 提交到仓库。

## 发版脚本

每次上游同步后发一版，用脚本一次完成版本号、变更记录和标签，避免手工漏改导致 tag 构建失败：

```bash
bun scripts/cut-release.mjs --dry-run   # 预览版本号与本次 CHANGELOG 条目
bun scripts/cut-release.mjs             # 写入 VERSION/CHANGELOG，提交并打 tag（不推送）
git push origin HEAD && git push origin vX.Y.Z.N
```

- 版本号必须与 tag 完全一致，脚本会拒绝重复版本、脏工作区、非 `main` 分支和落后于最新 tag 的历史。
- 只用正式版本号：Host Updater 会跳过 GitHub 上的 prerelease，带 `-rc` / `-beta` 的标签发布后后台检测不到。
- 脚本只提交 `VERSION` 与 `CHANGELOG.md`，不夹带其他改动。

## 备份与镜像保留

Host Updater 升级成功后会执行保留策略（`backend/internal/hostupdate/retention.go`），避免每次全量备份和旧镜像把磁盘写满：

| 配置 | 默认 | 作用 |
| --- | --- | --- |
| `CANVAS_UPDATER_BACKUP_RETENTION` | 3 | 保留最近 N 份 `backup-*.zip`，并始终保留状态里记录的最后一份 |
| `CANVAS_UPDATER_PRUNE_IMAGES` | true | 删除更早版本的业务镜像标签；仍被容器或回滚版本引用的镜像会被 Docker 拒绝删除并跳过 |

发版前仍要确认磁盘余量：单份全量备份约 1.4GB，保留 3 份意味着至少预留 5GB。
