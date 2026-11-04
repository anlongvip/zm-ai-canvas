# 画布组件结构

本文件写「画布上每种东西长什么样、由哪些部分组成、在什么状态下怎么变」。层级、事件和定位的合同见 `contracts.md`，颜色取值见 `theme.md`。

## 1. 节点

实现：`web/src/components/canvas/canvas-node.tsx`（外壳）、`canvas-node-content.tsx`（内容分发）、`canvas-node-media-content.tsx`（媒体）、`canvas-node-status-content.tsx`（生成中与失败）。

```
   [类型图标] 节点标题（11px，节点外上方，.canvas-node-external-header）
┌──────────────────────────────────────────────┐
○  .canvas-node-shell                           ○   ← 左右连接点（输入 / 输出）
│  data-node-state = idle|related|selected|      │
│                    target|focus                │
│  内容：媒体 / 文本 / 生成状态 / 空态            │
└──────────────────────────────────────────────┘
◢ 四角尺寸手柄（-14px 外扩命中区，选中时出现）
```

| 部分 | 规则 |
| --- | --- |
| 标题 | 在节点外上方，高 `--node-header-height`（24px），字号 `--node-title-fs`（11px），层级 `--node-z-overlay`。节点内部不再做标题栏。名称可单击铅笔编辑，双击和右键不能是唯一入口 |
| 外壳 | 圆角 `--node-radius`（12px），表面 `theme.node.fill`；默认 1px 透明描边占位，选中换 `theme.node.activeStroke`，不靠阴影或颜色表示选中 |
| 状态属性 | `data-node-state`：`focus`（Agent 聚焦相关）> `target`（正在作为连线目标）> `selected` > `related`（与选中节点相连，批量子节点除外）> `idle`。`data-state` 另存任务状态。样式按属性写，不在组件里拼颜色 |
| 连接点 | 左输入、右输出，悬停出现「+」圆点；`aria-label` 写「输入连接点，单击创建节点或拖动连线」。单击打开连线创建菜单，拖动连线 |
| 尺寸手柄 | 四角，命中区向外扩 14px，光标 `nwse-resize` / `nesw-resize`；层级 `--node-z-handle` |
| 内容层 | `--node-z-content`；媒体保持原始比例，不加遮罩；文本节点正文可点击选中 |
| LOD | `data-node-lod` = `shell` / `preview` / `full`，规则见 `SKILL.md` §2.6 |

节点类型色（`--cn-type-image`、`--cn-type-video`、`--cn-type-text`、`--cn-type-audio`、`--cn-type-script`、`--cn-type-character`、`--cn-type-frame` 等）只用于标题前的类型图标、类型徽标（`--node-type-badge-size` 20px）和小地图色块。

## 2. 节点状态

| 状态 | 画面 | 用户能做什么 |
| --- | --- | --- |
| 空节点 | 节点内轻量子表面（`theme.node.panel`）+ 图标 + 一句「下一步」 | 点击打开下方提示词面板 |
| 生成中 | `.canvas-gen-state`，`data-phase` 表示阶段；标题 + 已用时间 + 提示 + 三步进度「排队 / 生成 / 保存」+「查看详情」 | 查看详情；不阻止编辑其他节点 |
| 成功 | 媒体或文本本身，没有额外徽标 | 节点工具条操作 |
| 失败 | `.canvas-gen-state.is-failure`，`data-tone` = `warning` / `error`；图标 + 标题 + 一句说明 + 动作 | 见下表 |

**失败按「用户下一步做什么」分类**，而不是回显错误原文（`canvasFailureView`，`canvas-node-status-content.tsx`）：

| 类型 | 判断依据 | 标题 | 说明 | 主动作 | 次动作 | 色调 |
| --- | --- | --- | --- | --- | --- | --- |
| `uncertain` | 提交结果后端未确认 | 提交结果待确认 | 为避免重复扣费，未自动重试，可在详情里核对 | 无（只能看详情） | — | warning |
| `moderation` | 内容审核拒绝 | 未通过内容审核 | 上游给出的原因 | 无 | — | error |
| `recover-media` | 作品已生成但保存失败 | 作品保存未完成 | 作品已生成，重新保存不会重新生成 | 重新保存 | — | warning |
| `resync` | 任务可重新同步，且资源可重新获取 | 作品载入失败 | 作品已生成，重新获取不会重新生成 | 重新获取 | 重新生成 | warning |
| `resync` | 任务可重新同步，任务仍在进行 | 状态同步中断 | 任务仍在后台进行，重新获取不会重新生成 | 重新获取 | 重新生成 | warning |
| `failed` | 其他 | 生成失败 / 部分图片未生成（批量） | 用户可读的失败原因 | 重新生成 / 重新生成失败项 | — | error |

规则：
- 会再次扣费的动作（重新生成）只在确定失败时作为主动作；能免费恢复的情况先给免费动作。
- 动作按钮在 `onPointerDown` / `onMouseDown` 里阻止冒泡，点按钮不会拖动或选中节点。
- 原始错误放在 `title` 悬停和任务详情里，卡片上只写一句人话。
- 状态内容不改变节点尺寸；未完成节点按目标比例保持占位框（`web/src/lib/canvas/canvas-node-size.ts`）。
- 入场动画只在本次挂载期间看到「生成中 → 成功」时播放。

## 3. 节点工具条

实现：`web/src/components/canvas/canvas-node-toolbar.tsx`；工具来自注册表 `web/src/lib/canvas/tool-registry/`。

- 位置：节点上方，层级 `--z-node-toolbar`，根节点 `data-canvas-no-zoom`，阻止指针和键盘事件冒泡；用 `avoidCanvasNodePanels` 躲开已打开的面板。
- 样式：`canvasDockStyle(theme)`；按钮默认透明，悬停 `--dock-command-hover`，激活 `--dock-command-active`。
- 分组（`NodeToolbarGroup`）与菜单名：

| 分组 | 宽档 | 紧凑档（`layoutLevel >= 1`） | 窄档（`layoutLevel >= 2`） |
| --- | --- | --- | --- |
| `primary` | 直接显示 | 直接显示 | 只留第一个和旋转，其余进「更多」 |
| `adjust` | 「画面调整」菜单 | 并入工具菜单 | 并入工具菜单 |
| `camera` | 「镜头」菜单 | 并入工具菜单 | 并入工具菜单 |
| `sheet` | 「宫格与设定」菜单 | 并入工具菜单 | 并入工具菜单 |
| `process` | 工具菜单（视频为扳手图标） | 工具菜单，按 `section` 分组 | 同左 |
| `workspace` / `utility` | 直接显示 | 直接显示 | 进「更多」 |
| `more` | 「更多」 | 「更多」 | 「更多」 |

- 档位由**实测可用宽度**决定（`canvasToolbarLayoutLevel`），不由视口断点决定。折叠时入口只是移进菜单，不消失。
- 加一个节点工具：在 `definitions/node-hover-tools.tsx` 写一个 `ToolDefinition`：

```tsx
// 真实定义，摘自 definitions/node-hover-tools.tsx
{
    id: "createEditCopy",
    toolbar: "node-hover",
    category: "node-state",
    label: "创建编辑副本",
    displayLabel: "创建编辑副本",
    icon: <Images className="size-3.5" />,
    defaultVisible: true,
    defaultOrder: 195,
    nodeToolbar: { group: "process", order: 5, section: "编辑副本", description: "保留原图，连线复制后在副本上调整" },
    applicable: (ctx) => hasImage(ctx) && !ctx.nodeMetadata?.imageLayerGroup && !ctx.nodeMetadata?.imageLayer && !ctx.nodeMetadata?.layerExtraction,
    disabled: (ctx) => Boolean(ctx.nodeMetadata?.fileUpload) || !ctx.handlers.onNodeCreateEditCopy,
    run: (ctx) => ctx.handlers.onNodeCreateEditCopy?.(ctx.node!),
}
```

  - 新动作的回调加在 `ToolbarHandlers`（`ctx.handlers.onXxx`），由画布页面注入；工具定义里不 import 页面代码。
  - `nodeToolbar.group` 可以是函数：同一个工具在编辑副本上升为 `primary`，在原图上留在 `process`（旋转工具就是这样）。

  - `applicable` 返回 false：工具不渲染，用户设置也打不开。`disabled`：显示但不可用，要能说清原因。
  - 面板展开型工具设 `expands: true`（渲染 `aria-expanded`），持续开关用 `active`（渲染 `aria-pressed`），危险操作设 `danger: true`（单独分组）。
  - 互斥选项（例如框选 / 移动）用 `switchGroup`，渲染为分段切换。
  - 用户的显隐与顺序偏好由 `tool-persistence.ts` 按工具条保存；改工具 ID 时要在 `migrateToolbarPrefs` 里迁移。
  - 字段全集以 `web/src/lib/canvas/tool-registry/tool-definition.ts` 为准。合同测试 `web/test/canvas-node-toolbar.test.ts`（其中一条要求图片专属工具携带展示元数据，而不是靠组件里的 ID 列表）。

## 4. 提示词面板与放大编辑

实现：`web/src/components/canvas/canvas-node-prompt-panel.tsx`。

- 在节点下方打开（空间不够时由 `placeCanvasNodePanel` 移入可用区域），跟随节点拖动和画布缩放。
- 面板表面 `theme.node.panel`，边界 `theme.node.edge`，阴影 `theme.node.shadow`；模型选择、参考素材、放大编辑、参数折叠按钮的悬停用 `theme.toolbar.itemHover`，激活用 `theme.toolbar.activeBg`。面板根节点把这些值写成 `--cn-*` 变量供 CSS 使用。
- 放大编辑弹窗复用同一组表面、边界和阴影，节点内编辑和放大编辑只有一套视觉语言。
- 生成到当前节点；数量大于 1 时创建子节点。
- `@` 引用素材的菜单（`canvas-mention-menu.tsx`、`canvas-resource-mention-textarea.tsx`）带 `data-canvas-resource-mention-menu`，不参与画布滚轮；复制剪切要保留引用原文。
- 「将使用默认模型与参数」是状态说明，不是按钮。

## 5. 连线

实现：`web/src/components/canvas/canvas-connections.tsx`，粗细 `CANVAS_CONNECTION_STROKE_WIDTH`（1.4）。

- 命中层与视觉层分开：透明命中路径 16px 宽，视觉路径不接收事件。
- 默认：柔和外层 + 清晰内芯两层路径 + 两端小圆点，颜色 `theme.connection.stroke`。
- 悬停或选中：切换到 `theme.accent.primary`，加粗，流动虚线（`prefers-reduced-motion` 下不流动）。
- 所有路径 `vectorEffect="non-scaling-stroke"`，缩放时线宽不变。
- 拖动节点时由图形层逐帧画预览，同时隐藏静态 SVG 描边。
- 从连接点拖出松手在空白处，打开连线创建菜单（`data-connection-create-menu`）。合同测试 `web/test/canvas-connection-*.test.ts*`。

## 6. 四角浮动控件

以代码为准：`web/src/pages/canvas/canvas-project-top-bar.tsx`、`web/src/pages/canvas/project.tsx`。设计说明 `docs/design/canvas-floating-controls.mdx` 里的「右下工作模式切换」已过时：现在 `workspaceMode` 固定为 `professional`，没有模式切换控件。

```
┌ 画布菜单 · 名称 ✎ · 同步状态 ──── [画布 | 资产] ──── 搜索 导入 积分 专注 短剧 分享 ┐
│                                                                              │
│                                画布                              Agent 入口   │
│                                                                              │
└ 视图控制 · 小地图 ─────────────── 画布工具 Dock（居中）──────────────────────────┘
```

- 画布工具 Dock：`CanvasToolbar`（`web/src/components/canvas/canvas-toolbar.tsx`），底部居中，`bottom: var(--canvas-inset-y)`，根节点 `data-canvas-no-zoom`；内容来自 `main` 工具条注册表，外观面板从 Dock 上方展开。

- 顶栏：`CanvasTopBar`，高 `--canvas-topbar-h`（64px，窄屏 56px）。
  - 左：画布菜单（新建画布、删除当前画布、导入素材）、画布名称（点击可改）、「重命名画布」铅笔、同步状态图标（`canvas-sync-status.tsx`）。
  - 中（≥ xl 宽度才显示）：「画布 / 资产」视图切换，胶囊轨道里两个圆形图标按钮，当前项反色，`aria-pressed`。
  - 右：搜索画布节点、导入第三方画布（LibTV、TapNow）、积分中心、专注模式（Shift + ⌘/Ctrl + F）、短剧流程（画布关联短剧时）、分享画布。
  - 只放图标按钮，文字靠 Tooltip（顶栏文件内的 CanvasTopBarTooltip，portal 出去）和 `aria-label`。
- 左下视图控制：`web/src/components/canvas/canvas-zoom-controls.tsx`，快速缩放档 25% / 50% / 100% / 200%，「适应画布」把全部内容纳入视口。
- Agent 入口（右侧悬浮）：可拖动的形象，位置按 `canvas:agent-launcher-position:v1` 记住，离视口边至少 20px，窄屏（< 768px）和画布外高度封顶 120px（`web/src/lib/canvas/agent-launcher-position.ts`）。
- 所有浮动面板：`canvasDockStyle(theme)`，圆角 `--dock-radius`（14px），内边距 `--dock-padding`（8px），图标按钮 `--dock-icon-size`（32px），安全边距 `--canvas-inset-x` / `--canvas-inset-y`。
- 小地图（`canvas-mini-map.tsx`）：用于定位，不复制节点内容。可加载缩略图的图片节点 ≤ 24 个时显示缩略图，超过统一用类型色块；每个节点一条单行短标题。
- 多个浮层之间：`canvas-overlay-layer.tsx` 按点击和焦点把最后交互的浮层提到 `--z-canvas-overlay-active`。

## 7. 菜单与弹出层

| 组件 | 用于 | 行为 |
| --- | --- | --- |
| `CanvasAnchoredDropdown`（`canvas-anchored-dropdown.tsx`） | 一次性操作菜单：更多、切换模型、替换 | 保留 AntD 菜单键盘行为；锚点移动（节点被拖动、画布平移）就关闭 |
| `CanvasToolPopover`（`canvas-tool-popover.tsx`） | 持续面板：搜索、筛选、预览卡、参数 | 跟随锚点，空间不足时翻转和平移，离视口边 12px；外圈透明内边距不接收指针 |
| 添加节点菜单（`canvas-create-menu.tsx`） | 新建节点 | 节点区四列网格，无边框，靠表面明度分层；`workflow` / `project` / `resource` 各自成区 |
| 连线创建菜单 | 从连接点拉出后建节点 | 行高固定，说明统一在底部说明区，悬停不移动列表 |
| 上下文菜单（`canvas-context-menu.tsx`） | 右键 | 只放已有入口的快捷方式，不放独有功能 |

## 8. 节点弹窗

节点的参数型操作打开 `AppModal`，文件名 `canvas-node-<动作>-dialog.tsx`（裁剪、放大、打光、镜头、多角度、分割、局部重绘、图层拆分、文字编辑等）。

- 标题写动作（「调整图片尺寸」），主按钮写结果（「生成放大图」），未就绪时主按钮禁用并说明缺什么。
- 左边预览、右边参数；预览保持原图比例。
- 宽度按内容定（参考 `canvas-node-upscale-dialog.tsx` 760px），不做全屏。
- 弹窗里的下拉、提及菜单要能在弹窗之上显示并留在弹窗的焦点边界内（`--z-dialog-popover` / `--z-tooltip`）。
- 打开弹窗时 `useActiveTheme()` 已经是画布主题，不要再手动传明暗。
