# 影策页面范式

每个范式都对应一个已上线的真实页面。新页面先用 §0 选出最接近的范式，照搬骨架，再改内容。范式之间的差别在于**用户在这页上的主任务**，不在于长相。

每个范式按同一结构写：**用户任务 → 骨架 → 现有实现 → 必须遵守 → 常见错误 → 验收**。

## 0. 选范式

按顺序回答，第一个「是」就是你的范式：

1. 用户在这里**连续创作**、不停在节点之间跳转？→ §2 画布（另读 `skills/yingce-canvas-ui/SKILL.md`）
2. 用户**用一句话或一段对话**发起创作？→ §3 对话式创作入口（`/create`）
3. 用户**填参数 → 提交 → 看一批结果**？→ §4 创作工作台
4. 用户在**一条多步流水线**上推进同一个作品？→ §5 流水线作品（短剧）
5. 用户在**浏览、找、管理**一批同类东西？→ §6 内容库；如果每条是一个**进行中的过程**，→ §7 任务中心
6. 用户在**改自己的偏好或连接**？→ §8 用户设置
7. 用户**还没登录**？→ §9 登录与注册
8. 管理员在**查列表 / 改配置 / 看健康度**？→ §10 后台列表 / §11 后台设置 / §12 后台概览
9. 只是一个**局部交互**（确认、上传、批量、筛选、详情）？→ §13 局部范式

都不是：先停下来写清楚用户任务（`skills/yingce-product-design/SKILL.md` §1），多半是任务没想清楚，而不是缺一个新范式。

## 1. 工作台外壳（所有用户页面的容器）

**用户任务**：在不同工作区之间切换，随时知道账号、积分和通知。

```
┌ 侧栏卡片 250px（折叠 68px）┐ ┌ 主区卡片 .app-workspace-stage ─────────────────┐
│ 品牌                         │ │ 顶部栏：位置 · 通知 · 主题 · 账号（36px 图标按钮）│
│ 导航（分组）                  │ │ ───────────────────────────────────────────── │
│                              │ │ 页面内容（WorkspacePage）                      │
│ 签到 · 存储用量 · 账号卡       │ │                                               │
└──────────────────────────────┘ └───────────────────────────────────────────────┘
```

- 实现：`web/src/layouts/user-layout.tsx`、`web/src/components/layout/workspace-sidebar-nav.tsx`、`web/src/components/layout/workspace-top-bar.tsx`；宽度 token `--workspace-sidebar-width`（250px）、`--workspace-sidebar-collapsed-width`（68px），窄屏 200px。设计说明 `docs/design/workspace-shell-design.mdx`（其中写的 260px 已过时，以 token 为准）。
- 必须遵守：页面不自己画顶栏和侧栏；全局 body 状态只由布局设置；覆盖层不透明；暗色阴影用纯黑。
- 常见错误：在页面里再放一行「返回」「账号」按钮，和顶部栏重复。

## 2. 画布编辑器（`/canvas/:id`）

**用户任务**：在一张无限画布上把文字、图片、视频、音频串成作品。

- 骨架：全屏画布 + 四角浮动控件（左上项目、右上项目级操作、底部居中工具 Dock、左下视图控制）+ 节点上方工具条 + 节点下方提示词面板 + 右侧浮动面板。
- 必须遵守：画布不是普通页面，主题、层级、滚轮、指针归画布自己管。全部规则在 `skills/yingce-canvas-ui/`。
- 常见错误：把页面级的 `WorkspacePage`、`--user-*` 表面、`PageHeader` 搬进画布；在画布上放常驻的大表单面板。

## 3. 对话式创作入口（`/create`）

**用户任务**：用一句话开始一次创作，然后在同一个对话里迭代。

```
空状态（launchpad）                          对话状态（thread）
┌──────────────────────────────────┐         ┌ 工具栏：新对话 · 历史 · 镜头跳转 ┐
│ 标题                 查看历史对话 │         │ 消息流（结果卡在消息里）          │
│ [视频|图片|文本|Agent] 模式切换   │         │                                  │
│ ┌ 输入框（composer, empty）────┐ │   →     │                                  │
│ │ 参考素材 · 模型 · 参数   发送 │ │         │ ┌ 输入框（composer, thread）───┐ │
│ └──────────────────────────────┘ │         │ └──────────────────────────────┘ │
│ 灵感建议 · 最近画布 · 灵感大厅    │         └──────────────────────────────────┘
└──────────────────────────────────┘
```

- 实现：`web/src/pages/create/index.tsx`（两种状态切换）、`creation-workspace.tsx`（`CreationModeTabs` 视频 / 图片 / 文本，Agent 单独入口）、`creation-workspace-empty.tsx`、`creation-recent-canvases.tsx`、`creation-inspiration-tunnel.tsx`（灵感大厅，懒加载）、`creation-workspace-history.tsx`（`CreationHistoryDrawer`，按今天 / 昨天 / 本周 / 更早分组）。
- 必须遵守：
  - 首屏只有一个输入框是主角；灵感、最近画布在它下面，滚动后输入框收成浮动条（`creation-floating-prompt`）。
  - 模式切换改的是输入框的参数，不换页面。
  - 未登录可以浏览，提交时才弹登录（`CreationLoginDialog`），输入框里的内容不清空。
  - 模型价格放在发送按钮上，不单独占一行。
- 常见错误：首屏放功能介绍卡；把参数全部展开在输入框上方；在对话里用聊天气泡包住生成结果的每个字段。
- 验收：未登录 → 输入 → 提交 → 登录 → 回到原输入；空状态滚动后浮动输入框出现且不挡结果。

## 4. 创作工作台（输入面板 + 结果区）

**用户任务**：填参数 → 提交生成 → 看一批结果、挑出满意的。

**现有实现**：`/toolbox/image`、`/toolbox/video`（`web/src/pages/workbench/`，类名前缀 `wb-`）；`/ecommerce`（`web/src/pages/ecommerce/index.tsx`、`composer.css`，类名前缀 `ecs-`；结果区 `output-gallery.tsx`）。

```
┌───────────────────────────────┬──────────────────────┐
│ 结果区（minmax(0, 1fr)）        │ 创作面板（380–420px） │
│ ┌ 标题 · 计数     筛选 · 导出 ┐ │ ┌ 类型切换     图标操作┐│
│ │ 流程区：已发送需求/方向/审阅 │ │ │ 当前项目一行         ││
│ │ 批次卡 → 作品网格            │ │ ├──── 内部滚动 ───────┤│
│ │ 空态：下一步要做什么          │ │ │ 区块：标题 + 一句说明 ││
│ └─────────────────────────────┘ │ │ 区块之间 1px 分隔线   ││
│                                 │ ├──── 固定底栏 ───────┤│
│                                 │ │ 报价 · 张数   次要 主 ││
│                                 │ └─────────────────────┘│
└───────────────────────────────┴──────────────────────┘
```

必须遵守：
- 两栏左右顺序由页面定（工具箱面板在左，电商面板在右），同一页面内不变。
- 外层 `WorkspacePage fluid scroll={false}`；壳层 `display:grid; height:100%; gap: var(--space-4)`；面板 `border-radius: var(--r-2xl); background: var(--user-surface)`，满高，`overflow:hidden`。
- 面板分三段：头部（类型切换 + 图标按钮）、滚动区（`scrollbar-gutter: stable`，左右 16px 内边距）、底栏（`border-top: 1px solid var(--user-border)`，左边报价两行、右边按钮）。
- 区块 = `<section>` + `<header><h3>标题</h3><span>一句说明</span></header>` + 内容；区块之间 `border-top: 1px solid color-mix(in srgb, var(--user-border) 70%, transparent)`，上下内边距 16px。区块不再包卡片。
- 选项 ≤ 6 个平铺为三列选择块（`ecs-scene`、`wb-chip`），选中用 `--control-selected-*`，`aria-pressed`。
- 描述输入：`--user-surface-muted` 底、无边、聚焦时变 `--user-surface` + 1px `--user-border-strong` 内描边；下方可点选短语分类 tab + 胶囊短语（点一下写入描述，再点移除，见 `web/src/lib/prompt-phrases.ts`）。
- 模型：`ModelPicker variant="creation" fullWidth`；比例 / 清晰度为等宽选择块；数量为步进器。
- 结果区：作品卡只有媒体底色一层（`--user-surface-muted`），标题与操作排在媒体下方；多选勾选框悬停出现；状态文字在卡片右下角用颜色区分（失败 `--status-error`）。
- 窄屏 ≤ 760px：顶部 `SegmentedControl` 切换「创作 / 作品」，两栏只显示一栏。

常见错误：面板里每个区块再包一张卡（边框套边框）；把规则说明常驻在面板顶部；结果区空态写「暂无数据」。

验收：360 / 760 / 1280 / 1600 四个宽度下面板底栏始终可见；提交后结果区出现批次卡且面板不重置。

## 5. 流水线作品（短剧 `/projects`）

**用户任务**：把一本小说一步步变成成片，随时知道卡在哪一步、哪一集。

```
列表 /projects                     详情 /projects/:id
PageHeader：短剧 · 一句说明  新建     ┌ 顶栏：作品名  [制作|角色|待处理 N|设置]   精修画布 · 全部生成视频 · N 集 ┐
CollectionToolbar：搜索 · 筛选 · 重置 │ 队列条（QueueBar）：正在跑什么、失败几项、重试                          │
作品卡网格                           │ ┌ 剧集栏 EpisodeRail ┐ ┌ 制作区 drama-main ───────────────────────────┐ │
PaginationBar                        │ │ 第 1 集 · 状态      │ │ 当前集：剧本 → 分镜 → 镜头 → 成片             │ │
                                     │ │ + 加一集 · 选章节   │ │                                              │ │
                                     │ └────────────────────┘ └──────────────────────────────────────────────┘ │
                                     └────────────────────────────────────────────────────────────────────────┘
```

- 实现：`web/src/pages/projects/index.tsx`（列表）、`create.tsx`（新建，`WorkspacePage fluid scroll={false}`）、`detail.tsx`（详情）、`studio/`（`episode-rail.tsx`、`queue-bar.tsx`、`panels.tsx`、`storyboard.tsx`、`segment-dialog.tsx`、`final-view.tsx`）、样式 `drama.css`。
- 必须遵守：
  - 流水线状态是页面主信息：队列条始终在顶栏下方，待处理数量在导航项上用计数徽标（> 99 显示 99+）。
  - 「全部生成视频」写明集数，点击先确认；按钮在窄屏只留图标，文字进 `aria-label` 和 `title`。
  - 当前集和剧集栏同时可见；窄屏用 `SegmentedControl` 在「剧集 · N / 制作」之间切换，选一集后自动切到制作。
  - 需要精修时进画布（「精修画布」），精修结果采用回短剧，不在短剧页重做一套画布能力。
  - 作品不可用时整页用 `WorkspaceErrorState`（「作品不可用」+「返回作品列表」）。
- 常见错误：把每一步做成一个独立页面，用户来回跳；把所有集的所有镜头平铺在一页。

## 6. 内容库（项目 / 画布 / 素材 / 技能 / 插件 / 灵感）

**用户任务**：找到一个东西并对它做一件事；偶尔批量整理。

**现有实现**：`/projects`、`/canvas`、`/assets`、`/skills`、`/plugins`、`/inspirations`。表面规则见 `skills/yingce-ui-system/SKILL.md` §2。

```
页头：标题  [计数徽章]  一句说明                主操作 · 次要 · …更多
工具栏：搜索  选择器  分段筛选              重置 · 视图切换
网格：创建卡（真实首项） 内容卡 内容卡 …
分页
（选中时）批量操作栏
```

- 页头 `PageHeader`、网格 `CollectionGrid`、分页 `PaginationBar`（`web/src/components/layout/workspace-page.tsx`）。
- 工具栏：内容库用 `CollectionToolbar`（`web/src/components/layout/collection-toolbar.tsx`）：左边放搜索和选择器，有筛选时右边出现「重置」，`trailing` 放视图切换等操作；选择器自动用 `filled` 变体。`ListToolbar` 只在需要「已选筛选 chip」的列表里用（目前只有 Eagle 插件页）。
- 卡片同一表面 `--library-surface`，默认无边无影，hover 只换 `--library-surface-hover`，选中才有 1px `--library-selected-stroke`。不同库只改列数、媒体比例、信息量，不改表面。
- 空态用 `WorkspaceState` 系列：`WorkspaceState`（首次使用）、`WorkspaceNoResultsState`（筛选无结果，带「清除筛选」）、`WorkspaceErrorState`（加载失败，带「重新加载」）、`WorkspaceForbiddenState`（无权限）、`WorkspaceLoadingState`（加载中）。
- 详情：信息多、要并排看列表的用抽屉（素材 `AssetDrawer`，技能编辑 `SkillEditorDrawer`）；只读查看、看完就走的用弹窗（`SkillDetailModal`、`PluginDetailsModal`、`InspirationDetailModal`）。弹窗关闭后焦点回到打开它的卡片（插件页的 `restoreFocus`）。

各库的差别（只差在这里）：

| 库 | 文件 | 特有部分 |
| --- | --- | --- |
| 素材 | `web/src/pages/assets/index.tsx` | 「素材库 / 回收站」两种视图共用一页，页头标题和说明随视图换；类型与分类筛选带计数；批量上传弹窗（`AssetBatchUploadModal`）；批量栏（`AssetsBatchBar`）；回收站有「清空回收站」危险确认 |
| 技能 | `web/src/pages/skills/index.tsx` | 安装弹窗、推荐弹窗、编辑抽屉、详情弹窗 |
| 插件 | `web/src/pages/plugins/index.tsx` | 插件详情与开发文档弹窗（`plugin-documentation-modals.tsx`） |
| 灵感 | `web/src/pages/inspirations/index.tsx` | 用 `SegmentedControl` 按模式筛选；详情弹窗与编辑弹窗分开 |
| 短剧 | `web/src/pages/projects/index.tsx` | 见 §5 |

常见错误：每个库自己画一套卡片；工具栏塞进 5 个以上选择器（多了收进「筛选」）；批量操作常驻在页头。

## 7. 任务中心（`/tasks`）

**用户任务**：看正在跑的任务有没有卡住，失败的能不能一键重来。

```
PageHeader：任务 · 一句说明
CollectionToolbar：搜索 · 画布选择（全部画布 / 某画布） · 状态分段 · 视图（列表 / 网格）
状态分段：全部 N · 运行中 N · 已完成 N · 失败/取消 N
分组头（TaskGroupHeader）：按画布分组（画布名 · 所属项目） · 重试本组失败项
列表行（TaskListRow） / 网格卡（TaskGridCard）
PaginationBar
```

- 实现：`web/src/pages/tasks/index.tsx`、`task-status-filter.tsx`、`task-group-header.tsx`、`task-list-row.tsx`、`task-grid-card.tsx`。
- 必须遵守：
  - 状态分段带计数，「失败/取消」放在一起，因为用户对两者的下一步相同（重试或删除）。
  - 视图偏好记住（`readTaskPreference`），默认列表。
  - 任务按画布分组（`groupTasksByCanvas`），因为用户是按「哪张画布上的活」来想任务的。重试按组进行，重试期间该组按钮 loading，不阻塞其他组。
  - 运行中任务的时间实时走，终态时间冻结。
- 常见错误：把任务做成后台式表格，列出几十个技术字段；失败只显示错误码。

## 8. 用户设置（`/settings`）

**用户任务**：接上自己的模型渠道、改默认值，然后回去创作。

```
（从生成流程跳来时）顶栏：                        返回创作 · 保存并返回
┌ 分类导航 200px ┐ ┌ 内容 ─────────────────────────────────┐
│ 图标 名称       │ │ .settings-pane-header：标题 + 一句说明 │
│      一句描述    │ │ .settings-section                       │
│ …（10 个分类）   │ │   .settings-preference-block / SettingsRow │
└────────────────┘ └────────────────────────────────────────┘
```

- 实现：`web/src/pages/settings/index.tsx`（分类：个人信息、个人渠道、RunningHub 工作流、模型选择、生成偏好、提示词偏好、Agent 记忆、我的对象存储、问题诊断、我的反馈）；单行设置 `SettingsRow`（`web/src/components/ui/product/settings-row/`）。
- 必须遵守：
  - 导航项是 `button` + `aria-current="page"`，窄屏横向滚动。
  - 每个分类一个 pane：标题 + 一句说明 → 分区 → 字段。字段用 `SettingsRow`：左标签和说明、右控件。
  - 偏好类设置改了就生效（写入本地配置），不放保存按钮。只有从生成流程被引导过来（缺渠道或模型）时，顶栏出现「保存并返回」，它会校验至少有一个可用渠道，不满足时跳到对应分类并说明缺什么。
  - 密钥字段不回显明文。
- 常见错误：每个设置包一张卡；一个分类里混入另一个分类的设置；保存按钮和即时生效混用而不说明。

## 9. 登录与注册（`/login`、`/register`、`/forgot-password`）

**用户任务**：尽快进去。

```
┌ 品牌影片区 auth-scene-hero ───────┐┌ 表单区 auth-scene-form-pane ──── 返回首页 ┐
│ 品牌                   播放影片   ││          ┌ auth-scene-card ──────────┐   │
│                                   ││          │ 眉题                       │   │
│ 标题（后台可配）                   ││          │ [密码登录 | 验证码登录]     │   │
│ 说明（后台可配）                   ││          │ 字段 · 主按钮               │   │
│                                   ││          │ 第三方登录（Linux.do）      │   │
└───────────────────────────────────┘└──────────┴───────────────────────────┴───┘
```

- 实现：`web/src/pages/auth/auth-scene.tsx`（共用场景）、`login.tsx`、`register.tsx`、`forgot-password.tsx`。品牌名、标题、说明、影片都来自后台「站点及外观」，不写死。
- 必须遵守：登录方式多于一种才出现 `Segmented`（`block`，`aria-label="登录方式"`）；提交中禁用切换；窄屏品牌区缩到 250–320px 高，表单在下。
- 常见错误：在登录页放产品功能介绍；错误提示只写「登录失败」。

## 10. 后台列表页

**用户任务**：找到一条记录，处理它。

后台是独立视觉系统：token 只用 `--admin-*`（`web/src/pages/admin/theme/admin-tokens.css`），不得使用 `WorkspacePage`、`AppModal`、`AppDrawer` 和用户端控件。规范全文 `docs/design/admin-ui-design-standard.md`。

```
AdminPageFrame：分组 / 当前页面（h1） · 一句说明              页头操作 · 主题切换
工具栏：搜索（app-list-search，aria-label 写资源名） · 状态 128px · 类型 160–176px     重置
┌ 表格卡片（填满剩余高度，内部滚动）──────────────────────────────────┐
│ 表头 36px（底色与数据区不同）                                         │
│ 数据行 40px：主信息左对齐 · 短字段居中 · 行操作居中（≤ 3 个外露）       │
│ 分页 44px（固定在卡片底部）                                           │
└────────────────────────────────────────────────────────────────────┘
```

- 实现：`AdminPageFrame`（`web/src/pages/admin/components/admin-shell.tsx`）、`AdminDataTable` / `AdminRowActions` / `AdminStatusBadge`（`web/src/pages/admin/components/admin-ui.tsx`）。例子：`web/src/pages/admin/users/users-page.tsx`、`web/src/pages/admin/login-logs/login-logs-page.tsx`、`web/src/pages/admin/plugins/plugins-page.tsx`。
- 必须遵守：选择框直接显示当前值，不再生成筛选标签；只显示当前状态下可执行的操作，不用一排禁用按钮占位；编辑资料、调积分、启停这类高频操作不收进「更多」；创建和重表单进带固定底栏的抽屉或弹窗。
- 常见错误：表格外再套说明卡；每行 5 个以上按钮。

## 11. 后台设置页

**用户任务**：看清当前状态和影响，改一处，确认生效。

```
AdminPageFrame：分组 / 当前页面 · 一句说明
状态操作栏（全宽）：功能状态 · 配置来源 · 更新时间                 刷新 · 撤销 · 保存修改
┌ 主配置流 ──────────────────────────────────────┐ ┌ 配置摘要 ~344px（≥1600px）┐
│ SettingsSectionCard                              │ │ 2×2 指标：名称 + 值         │
│ ┌ 说明单元 ~264px ┐│ 控件区（2–4 列网格）        │ │ 随滚动保持可见              │
│ │ 图标 标题 状态   ││ 开关 / 互斥选择 / 表单       │ └────────────────────────────┘
│ │ 一句用途         ││                              │
│ └─────────────────┘│                              │
└──────────────────────────────────────────────────┘
```

- 实现：`SettingsSectionCard`、`AdminStatTile`（`admin-ui.tsx`）。例子：`web/src/pages/admin/components/access-settings-panel.tsx`（登录与注册）、`web/src/pages/admin/components/feature-availability-panel.tsx`（功能开放）、`web/src/pages/admin/ecommerce-agent-config-page.tsx`（电商 Agent 配置）。
- 必须遵守：
  - 说明单元和控件区同一张卡片底色，只用纵向细分隔线区分，不做成卡片内侧栏。
  - 写入语义分两种，页面上要说清楚：**即时写入**（布尔开关、互斥选择，点了就存，失败恢复原状态）和**草稿写入**（文本、密钥、规则，点一次「保存修改」提交）。不能混着不说。
  - 有草稿时提供撤销、刷新覆盖确认、浏览器刷新与后台导航保护。
  - 保存响应要校验与提交一致；失败不自动重试。
  - 密钥不回显，留空表示保留原值。
  - 1600px 以下摘要变成状态栏下方的横向四格，不缩窄主配置区。
- 每个后台页面保持独立的侧栏入口，不用页头标签把几个页面合成一页（规范原文：页签入口不易发现）。`web/src/pages/admin/ecommerce-management-page.tsx` 用 `AdminSegmented` 合并了「知识 / 技能」，属于已知例外，新页面不要照做。
- 旧路由改名后保留 `Navigate` 重定向。

## 12. 后台概览页

**用户任务**：一眼知道系统健不健康、哪里要处理。

- 首屏依次回答：当前健康度 → 变化趋势 → 待处理事项。
- 工具栏：7 天 / 30 天 / 本月快捷范围 + 明确日期范围；低频条件收进可展开筛选；刷新与导出在右侧。
- 健康指标固定四域：用户活跃、生成任务、服务质量、估算费用。从属信息进对应指标卡，不另起一行。日变化写「较前一日」；当天无数据时不显示 ↓100%。
- 趋势区在「用量 / 质量 / 活跃」之间切换，一次只显示相关曲线，高约 280px；≥ 1280px 时趋势与「需要关注」约 2 : 0.82 并排。
- 实现：`web/src/pages/admin/components/analytics-panel.tsx`、`web/src/pages/admin/components/analytics-trend-delta.ts`。

## 13. 局部范式

| 场景 | 做法 | 实现 / 参考 |
| --- | --- | --- |
| 危险确认（不可恢复） | 就地 `Popconfirm`：标题问句写对象，说明写后果，确认按钮写动作并 `danger` | 素材页「确定清空回收站吗？」 |
| 危险确认（影响多个对象或计费） | `App.useApp().modal.confirm` 或自建确认弹窗，列出对象、数量、后果；提交中锁定 | 短剧「全部生成视频 · N 集」；后台写操作见 §11 |
| 可恢复的删除 | 不确认，直接移入回收站，Toast 说明结果；回收站里可以还原 | 素材「移入回收站 / 还原」 |
| 批量操作 | 选中后出现批量栏（数量 + 操作 + 取消选择），不常驻；全选跨页时明确「已选全部 N 项」 | `AssetsBatchBar` |
| 上传 | 拖入或点击；进度在条目上；失败条目可单独重试；大批量用弹窗并选目标文件夹 | `AssetBatchUploadModal`、`web/src/components/canvas/canvas-upload-modal.tsx` |
| 选模型 | `ModelPicker`；价格和能力跟着模型显示，不另写说明 | `web/src/components/model-picker.tsx` |
| 筛选 | 选择器直接显示当前值；有筛选时出现「重置」；无结果用 `WorkspaceNoResultsState` | `CollectionToolbar` |
| 二选一 / 多选一的持久状态 | `SegmentedControl`，`aria-pressed` 或 `aria-current` | 任务视图、灵感模式 |
| 长说明、规则、免责声明 | `<details>` 默认折叠 | 电商规则说明 |
| 详情 | 要边看边改：抽屉；只读看完就走：弹窗 | §6 |
| 进度 | 一行 `role="status"`（图标 + 文字）；失败 `role="alert"` + 原因 + 重试 | 电商流程卡 |
| 计数徽标 | 0 不显示，> 99 显示 99+，红色只给需要处理的数 | 短剧「待处理」 |

## 14. 弹窗

- 产品弹窗用 `AppModal`（后台 `AdminModal`）。内容自带外壳时 `flush`。
- 结构：标题（16px）+ 一句副说明 → 单层内容（分组用小标题，不套卡片）→ 底栏（左：当前选择摘要；右：取消 / 主操作）。
- 大量选项的选择弹窗（平台、物料）：分组标题 + 等宽网格；每个选项固定尺寸的图标容器 + 名称，选中用 `--control-selected-*`。参考 `web/src/pages/ecommerce/market-picker.tsx` + `market-picker.css`。
- 不要用 `Modal.confirm` 静态方法（lint 会拦）；确认用 `App.useApp().modal` 或原位确认。

## 15. 流程卡（Agent 推荐、方案审阅）

- 出现在结果区顶部，按时间顺序：已发送的需求 → 推荐方向（可选一个）→ 方案审阅（`<details>` 默认折叠，展开逐张可改）→ 生成按钮。
- 进度用一行 `role="status"`（转圈图标 + 文字），失败用 `role="alert"` 整块淡红底 + 原因 + 重试按钮。
- 不用聊天气泡模拟对话，除非页面本身是对话（`/create`、画布 Agent）。
