# 本地 Linux 验证环境

## 用途与边界

上游同步 PR #8 使用独立的 WSL 2 Ubuntu 24.04 环境 `zm-ai-canvas-ci`，在 Linux 文件系统内验证 Git 提交。Windows 工作目录仍是源码编辑入口。Linux 副本按提交快进同步，不读取 Windows 的 `node_modules`，不使用生产数据库、渠道配置或用户数据。

Windows 默认的 CRLF 换行和路径分隔符会影响源码文本断言、Go 格式检查及 Shell 测试，因此最终推送前检查在 Linux 副本执行。不要通过放宽断言或跳过检查解决这些环境差异。

## 已安装组件

| 组件 | 版本或用途 |
| --- | --- |
| Go | 1.25.0，开启 CGO，配套 GCC |
| Bun | 1.3.9，与 Quality checks 一致 |
| Node.js | 22.19.0，与后端 CI 一致 |
| PostgreSQL | 16，独立的 `canvas_test` 测试库 |
| Redis | 7，测试自行启动临时实例 |
| golangci-lint | 2.5.0，Agent 静态检查 |
| Google Chrome | Headless 浏览器验收 |
| 其他依赖 | Git、curl、Python 3、FFmpeg、pkg-config |

Go、Bun、Node 和 golangci-lint 下载包已核对官方 SHA256。前端使用 `bun install --frozen-lockfile`，Pi Agent 使用 `npm ci --omit=dev --ignore-scripts`；未更新依赖锁文件。

## 运行方法

本机提供 Git 忽略的 `.local/verify-linux.ps1`。先提交要验证的改动，然后在仓库根目录执行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .local/verify-linux.ps1
```

入口会先 fetch，拒绝脏工作区或落后远端的分支，再将当前提交快进同步到 Linux 副本。它启动独立 PostgreSQL 测试服务，加载固定工具链，并执行仓库的默认范围检查：

```bash
source /opt/canvas-ci/env.sh
cd /opt/canvas-ci/repo
bun scripts/verify-before-push.mjs --jobs 4
```

Linux 副本和工具位于 `/opt/canvas-ci/`；环境变量只指向本地测试库。总日志回传 Windows 的 `.local/cache/linux-verify.log`，各项明细保存在 Linux 副本 `.local/cache/verify/`。

## 验证与限制

- 已完成 Linux 全量前端测试：2942 项通过，0 项失败。
- 最终提交仍须通过上述默认范围检查，再推送并确认对应 SHA 的 GitHub Actions。
- 支付插件双架构构建由 GitHub Actions 验证，本地未安装 Docker/QEMU。
- 本环境只验证源码与测试，不代表生产升级、真实渠道生成或账务退款验收完成；服务器继续使用原部署版本。

## 维护与恢复

WSL 重启后通过入口重新启动 PostgreSQL。新增依赖先按锁文件安装；不要混用 Windows 与 Linux 的依赖目录。若远端分支变化，先在源码仓库整合，再同步并验证。

环境与业务数据隔离，停用时可停止 `zm-ai-canvas-ci`；不要注销或清理机器上已有的其他 WSL 发行版。业务代码的回滚仍按 PR 和发布版本操作，本环境不自动修改生产。
