# 官方更新同步流程

本仓库使用两个远程：

- `origin`：`anlongvip/zm-ai-canvas`，保存二开代码和发布分支；
- `upstream`：`ddcat-ai/open-ai-canvas`，只用于读取官方更新。

二开功能集中在 `codex/runninghub-video-phase1` 及后续 `codex/` 分支。不要直接在 `main` 上开发，也不要用官方更新覆盖二开分支。

## 首次保存二开基线

在 `E:\zm-ai-canvas` 执行：

```powershell
git status
git add backend web docs plugin-packages
git commit -m "feat(runninghub): 接入视频系统渠道与运行时间计费"
git push -u origin codex/runninghub-video-phase1
```

提交前需要配置你自己的 Git 作者信息：

```powershell
git config user.name "你的 GitHub 名称"
git config user.email "你的 GitHub 邮箱"
```

## 同步官方更新

每次官方发布新版本后：

```powershell
git switch codex/runninghub-video-phase1
git fetch upstream
git merge upstream/main
```

如果出现冲突，只处理 Git 标记出的代码冲突。RunningHub Provider、计费和插件包属于二开范围，合并后必须保留；官方无关模块按上游更新。

合并后执行最小验证：

```powershell
cd backend
go test ./internal/provider/runninghub ./internal/protocol
go test ./internal/app -run TestRunningHub -count=1
cd ..\web
bun test test/model-protocols.test.ts
bun run build
```

验证通过后推送：

```powershell
cd E:\zm-ai-canvas
git push origin codex/runninghub-video-phase1
```

## 发布与线上数据

线上发布使用二开分支构建的新版本。发布前备份数据库、OSS 和环境配置；更新代码或镜像时保留原数据库和数据卷。品牌、账号、积分、素材和 OSS 文件不在 Git 仓库中，不随代码同步或回滚。

禁止把 `.env`、API Key、数据库、上传文件和 `.settings-key` 提交到仓库。
