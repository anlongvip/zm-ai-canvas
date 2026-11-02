# 服务动态更新设计

## 目标与边界

动态更新面向使用官方 Docker Compose 部署的自托管实例。管理员可在后台查看当前版本、GitHub Release 新版本和更新日志，并发起一次受控升级。升级必须满足：同一时刻只有一个更新任务、数据库和数据目录有可恢复备份、迁移失败不启动新服务、健康检查失败自动恢复旧镜像、业务 Backend 不直接持有 Docker Socket。

源码部署、Kubernetes、Portainer 或用户自行修改过编排文件的实例第一阶段只提供版本检查和手工升级指引，不自动接管宿主机。

## 组件边界

```text
管理后台
  -> Backend 更新 API（鉴权、状态展示、审计）
      -> Host Updater（宿主机最小权限守护进程）
          -> GitHub Release / OCI Registry
          -> Docker Compose
          -> PostgreSQL / 数据卷备份
```

- Backend 负责管理员权限、Release 信息缓存、更新任务展示和审计，不执行 Docker 命令。
- Host Updater 负责下载、验签、备份、迁移、切换、健康验证和回退；只接受本机 Unix Socket 或随机高强度 Token 鉴权请求。
- `migrate-schema` 是唯一结构迁移入口。生产 Backend 使用 `CANVAS_AUTO_MIGRATE=false`，只校验 Schema 与当前二进制兼容。
- 官方 Release 发布不可变镜像摘要和更新清单；Updater 不执行 Release 文本中的任意命令或脚本。

## Release 清单

每个正式 Release 附带机器可读的 `update-manifest.json`，至少包含：

| 字段 | 用途 |
| --- | --- |
| `version`、`channel`、`publishedAt` | 版本比较和稳定版/测试版通道 |
| `minimumUpdaterVersion` | 阻止旧 Updater 执行不理解的新协议 |
| `images` | Web、Backend 镜像及不可变 digest |
| `schemaVersion`、`minimumRollbackSchemaVersion` | 升级前兼容性和可回退性判断 |
| `composeRevision` | 确认当前部署模板是否受支持 |
| `releaseNotesUrl` | 后台展示更新日志 |
| `signature` | 对规范化清单做签名，防止供应链篡改 |

版本比较使用语义化版本，正式通道默认忽略 prerelease。GitHub API 使用 ETag 和超时缓存，网络失败只显示“检查失败”，不会影响当前服务。

## 升级状态机

```text
idle -> checking -> preflight -> draining -> backup -> pulling
     -> migrating -> switching -> verifying -> succeeded
                       |             |            |
                       +---------- failed --------+
                                      |
                                  rolling_back
                                      |
                         rolled_back / manual_intervention
```

更新任务和每一步结果持久化到宿主机，不只保存在正在被替换的 Backend 中。Updater 使用文件锁保证单实例执行，并为每次更新生成 operation ID，重复点击只返回同一任务。

### 预检

1. 校验管理员请求、更新通道、Release 签名、镜像 digest 和磁盘空间。
2. 确认当前 Compose revision、数据目录、数据库连接和备份目录可访问。
3. 检查当前 Schema、目标 Schema 与回退兼容窗口；不满足时在停机前拒绝更新。
4. 拉取目标镜像但不切换，确认目标 Backend 内含期望版本的 `migrate-schema`。

### 排空与备份

Updater 先让 Backend 进入 drain。Readiness 立即变为 503，新生成任务返回可重试错误；周期 Worker 停止领取任务，已执行任务在 `CANVAS_SHUTDOWN_TIMEOUT` 内完成。随后执行 PostgreSQL 一致性备份、SQLite 文件快照和数据目录增量快照，并记录备份校验和。备份失败不得继续迁移。

Backend drain 同时关闭新 Agent 会话准入。模型任务完成并不代表远程 Agent 已消费响应并保存最终会话；因此停止平台 Worker 后，还需在同一退出期限内等待 Agent 会话写完检查点。超时返回退出错误，关闭路径再取消并等待剩余会话。恢复已创建的模型步骤先接管旧任务，再检查新步骤预算，不为接管创建第二笔任务或订单。

`TestProductActiveAgentRollback` 可在上述独立产品镜像测试环境中验证该边界：挂起模拟模型响应，在 Backend 主 HTTP 停止后释放，要求最终备份前任务结算和含回答的会话快照已提交，再触发真实迁移失败并回退。可选 `CANVAS_HOSTUPDATE_PRODUCT_BACKEND_BINARY` 和 `CANVAS_HOSTUPDATE_PRODUCT_AGENT_SCRIPT` 只读挂载绝对路径的普通本地 Backend/Node 文件，日志记录其 SHA-256；该模式仅用于发布前验证，不能记作正式镜像或生产已修复。

该修复已随 `v1.7.3.7` 正式发布并部署。关闭本地二进制覆盖、使用正式三镜像的隔离验收通过（82.81 秒、退出码 0），证明备份前最终回答与单次结算已提交，回退后快照保留且没有再次调用模型。生产升级、最终备份完整性和历史视频只读复验通过；具体 Release、操作和证据见 `docs/plans/runninghub-video-phase1.md`，不将隔离故障注入表述为生产故障测试。

后续多步工具补丁将维护期的新模型步骤准入拒绝转换为内部暂停，先保存工具结果与会话，再退出等待重启恢复；已创建步骤仍优先接管。`TestProductAgentToolRoundRollback` 的发布前补丁容器验收通过（83.25 秒），最终备份时保留未结束的 run 与成功工具结果，真实迁移失败回退后只继续下一步，不重复工具或结算。此修复已通过 PR #7 正式发布并部署 `v1.7.3.8`。文本上游无可恢复编号且提交结果不确定时，恢复必须停止自动重发、保留冻结积分并进入待核对，不能为了让 run 完成而绕过提交幂等保护；超时/强杀专项分别验证该边界。

最终源码补丁的三项容器验收通过（77.63 / 85.27 / 80.24 秒、退出码 0）：多步回退继续第二步；SIGKILL 和实际 1 秒停服期限耗尽后，只保留一个失败任务、一个待核对订单和原冻结积分，上游调用一次。后两者不是自动完成证据；不能把无可查询编号的文本请求不确定状态当成确认失败或自动退款。

正式 `v1.7.3.8` 固定到 `f6c4f337b3b92212367889eec80cba743d105632`；关闭 Backend/Node 覆盖后的多步工具回退复验通过（82.18 秒、退出码 0），恢复后两个模型步骤、一次工具结果、两笔结算及最终会话保存。独立空库与内部模拟上游的测试资源已清理。生产于北京时间 2026-11-02 08:34:57 升级成功，实际三镜像及更新器摘要一致、五服务 healthy；最终 ZIP 摘要与全项 CRC 复验通过。此轮没有恢复生产备份或对生产注入故障，详细发布、部署与证据见 `docs/plans/runninghub-video-phase1.md`。

### 迁移与切换

目标镜像以一次性容器执行 `migrate-schema up`。迁移整批运行在事务中，PostgreSQL 使用事务级 advisory lock；迁移实现与 `schema_migrations` 记录的名称或 checksum 不一致时立即失败。迁移成功后才切换 Web/Backend 镜像，并保持旧镜像 digest 与旧 Compose 配置可用。

### 健康验证

切换后依次验证：

1. `/api/health/startup` 成功；
2. `/api/health/ready` 连续多次成功；
3. `/api/system/version` 与目标 Release、Schema 版本一致；
4. 可选的登录态只读冒烟检查成功。

单次 200 不视为升级成功。只有在稳定窗口内连续通过后才标记成功并清理临时资源。

`manager_health_test.go` 通过 Go 1.25 虚拟时钟执行完整十分钟验证窗口，覆盖 503、版本不符、业务错误、请求超时及稳定窗口失败后重置；HTTP 为模拟，不是 Docker 实时等待十分钟。`TestProductContainerRollback` 可设置更高固定 `CANVAS_HOSTUPDATE_PRODUCT_TARGET_VERSION`，只运行跨版本真实迁移失败回退；正式缓存镜像 `v1.7.3.6 → v1.7.3.7` 已通过（76.80 秒），核对恢复来源数据库、文件、密钥摘要和实际 readiness。

## 回退策略

回退分为两类：

- **镜像回退**：目标版本未修改 Schema，或新 Schema 仍兼容旧 Backend，直接恢复旧镜像和 Compose 配置并再次验证健康状态。
- **数据回退**：迁移包含破坏性或不可向后兼容变更，停止所有业务容器后恢复数据库和数据目录快照，再启动旧镜像。该过程会丢弃升级开始后的新写入，因此升级期间必须保持写流量关闭。

自动数据回退必须先校验同一份 ZIP 中的 PostgreSQL dump 与 Backend `/data` tar；按旧 Compose 和环境配置恢复数据库及数据目录后，才允许启动旧服务。任一停止、配置恢复、备份校验、数据库恢复、数据目录恢复或启动步骤失败，都应进入人工介入状态并保持业务服务停止，不能用部分恢复的数据启动。

当前实现恢复失败即中止，不继续启动业务；备份失败后重启旧服务、回退启动部分成功或健康验证失败时，再尝试停止 Web、Backend、Agent，并保留停服错误供人工处理。`pg_restore` 使用原 Compose 项目名称和 `--exit-on-error`。数据 tar 拒绝绝对路径、父目录分量、重复路径、非目录祖先及特殊设备条目，硬链接只能指向归档内普通文件；符号链接目标也不得包含 `..`，即使词法计算似乎仍在数据目录内。包含这些条目的备份需要人工检查，不能跳过验证后自动恢复。

故障验证分两层：默认 `go test ./internal/hostupdate -count=1` 使用模拟命令执行器；在具备 Docker Compose 和本地 `postgres:17-alpine` 镜像的隔离 Linux 环境，可显式设置 `CANVAS_HOSTUPDATE_DOCKER_TEST=1`，运行 `go test ./internal/hostupdate -run '^TestDockerRollback$' -count=1 -timeout=15m`。后者使用随机独立 Compose 项目与数据卷，不发布宿主机端口，测试结束清理自身容器与卷；真实执行 PostgreSQL dump/restore、数据 tar 和容器启停，覆盖迁移失败、备份失败、备份后旧服务启动失败、数据库恢复失败、数据恢复失败及回退部分启动失败。镜像仓库响应和 HTTP 健康响应为模拟，业务容器是轻量替身，因此不能替代真实产品迁移、Agent 排空或业务健康验收。

迁移设计默认采用 expand/contract：先新增字段/表并保持旧版本可读，至少跨一个发布周期后再清理旧结构。无法做到向后兼容的 Release 必须在清单中标记维护窗口和数据回退要求，后台需要二次确认。自动回退失败时保持服务停止并输出明确的备份位置、失败步骤和人工恢复命令，不反复重试破坏现场。

## 后台能力

第一阶段后台提供：当前版本、目标版本、发布时间、更新日志、通道、检查时间、升级前检查结果、更新进度、失败原因、回退结果和审计记录。只有超级管理员可发起更新；普通管理员只读。危险状态不提供“强制跳过迁移/备份/验签”入口。

建议 API：

- `GET /api/admin/system/update-status`
- `POST /api/admin/system/check-update`
- `POST /api/admin/system/update`
- `GET /api/admin/system/update-operations/:id`
- `POST /api/admin/system/update-operations/:id/rollback`

## 分阶段实施

### P0：可升级运行基础

- 权威 BuildInfo 与 `/api/system/version`；
- 分层健康检查和 Worker drain；
- 版本化、事务化的 `schema_migrations` 与独立迁移命令；
- 生产 Compose 的 migrate job、停止宽限期和 Release 版本门禁。

### P1：只读检查更新

- Backend 缓存 GitHub Release/manifest；
- 管理后台展示版本、更新日志、部署类型和自动更新可用性；
- 无 Host Updater 的部署只展示手工更新指引。

### P2：Host Updater 与手动升级

- 独立最小权限进程、操作锁、持久化状态、备份、镜像切换和健康回退；
- 后台实时展示进度，先在 PostgreSQL 官方 Compose 上灰度；
- SQLite 和自定义数据目录在备份/恢复验证完成后开放。

### P3：自动调度

- 支持维护窗口、稳定版通道、失败通知和延迟发布；
- 默认关闭无人值守升级；涉及破坏性迁移的版本始终要求人工确认。
