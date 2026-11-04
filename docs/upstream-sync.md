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

本轮同步分支以 `origin/main` 的 `v1.7.3.12` 为基线，目标作者提交为 `6c1532b4`，Schema 升至 79。该同步尚未发布或部署；用户此前报告服务器为 `v1.7.3.11`，当前线上状态尚未重新核验。完成同步验证后，仍需单独完成 PR、Release 与生产升级验收。

禁止把 `.env`、API Key、数据库、上传文件和 `.settings-key` 提交到仓库。
