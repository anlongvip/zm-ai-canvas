---
name: yingce-canvas-ui
description: 影策画布（/canvas/:id）组件规范。给画布加节点、节点工具、浮动面板、菜单、弹窗、连线样式，或改画布配色、层级、滚轮与指针行为时使用：画布主题归属、层级梯子、事件边界、滚轮所有权、浮层避让、节点状态与失败分类、LOD，以及这些规则各自防住的历史缺陷。
---

# 影策画布组件规范

> 开工前先按 `skills/SKILLS.md` §0 回答四个问题（专家会主动做什么、原始素材、方法与工具、你我之外的证据），再复述任务。要的是更深入，不是更大。

画布不是一个普通页面。它自己拥有**主题、层级、滚轮、指针和焦点**，页面级的规则（`WorkspacePage`、`--user-*` 表面、页面滚动）在这里大多不适用。画布上的大部分缺陷不是「样子不对」，而是浮层抢了滚轮、菜单跟丢锚点、面板挡住节点、主题串到工作台。所以本规范先讲合同，再讲外观。

## 地图

| 文件 | 回答什么 | 什么时候读 |
| --- | --- | --- |
| 本文件 §1 | 加一个东西，应该放在哪一层、用哪个组件 | 每次动画布前 |
| 本文件 §2 | 六条硬合同：主题、层级、事件边界、滚轮、浮层、性能 | 每次动画布前 |
| 本文件 §3 | 历史缺陷 → 规则对照，知道每条规则防的是什么 | 想绕开某条规则时 |
| `references/anatomy.md` | 节点结构、节点状态与失败分类、节点工具条分组、连线、提示词面板、四角浮动控件 | 加节点类型、改节点外观、加节点工具 |
| `references/contracts.md` | 层级梯子全表、事件选择器原文、滚轮算法常数、浮层避让函数、LOD 阈值、键盘与焦点 | 写浮层、菜单、可滚动区域、拖拽 |
| `references/theme.md` | `canvasThemes` 明暗两套取值与每个字段的用途 | 选颜色、写节点或工具条样式 |

代码入口：页面 `web/src/pages/canvas/project.tsx`，视口 `web/src/components/canvas/canvas-viewport.tsx`，节点 `web/src/components/canvas/canvas-node.tsx`，主题 `web/src/lib/canvas-theme.ts`，工具注册 `web/src/lib/canvas/tool-registry/`。设计说明：`docs/design/canvas-floating-controls.mdx`（部分过时，见 `skills/SKILLS.md` §5）、`docs/design/canvas-node-visual-contrast.mdx`、`docs/design/canvas-final-performance-architecture.md`。

## 1. 加一个东西，放哪里

先回答「它跟着什么走」：跟着节点走、跟着视口走，还是跟着页面走。

| 要加的东西 | 跟着 | 放在 | 用什么 |
| --- | --- | --- | --- |
| 对单个节点的一次性操作（放大、裁剪、去背景） | 节点 | 节点工具条 | 在 `web/src/lib/canvas/tool-registry/definitions/node-hover-tools.tsx` 注册 `ToolDefinition`，指定 `nodeToolbar.group`；组件里不按工具 ID 排版 |
| 需要参数的节点操作 | 节点 | 工具条按钮 → 弹窗 | `AppModal`，命名 `canvas-node-<动作>-dialog.tsx`，参考 `web/src/components/canvas/canvas-node-upscale-dialog.tsx` |
| 节点上的「更多」「切换模型」这类菜单 | 锚点 | 下拉菜单 | `CanvasAnchoredDropdown`（锚点一动就关） |
| 搜索、筛选、预览卡这类持续面板 | 锚点 | 弹出层 | `CanvasToolPopover`（跟随锚点，离视口边 12px） |
| 新的节点类型入口 | 视口 | 添加节点菜单 | 在 `web/src/lib/canvas/tool-registry/definitions/add-node-menu-tools.tsx` 注册 `AddNodeMenuCommand`，选 `section` |
| 画布级命令（整理、框选模式、外观） | 视口 | 底部 Dock | `main-toolbar-tools.tsx` 注册；互斥选项用 `switchGroup`，不要做成两个按钮 |
| 多选后的批量操作 | 选区 | 选区工具条 | `selection-toolbar-tools.tsx` 注册 |
| 项目级入口（分享、导入、积分、专注） | 页面 | 右上顶栏 | `web/src/pages/canvas/canvas-project-top-bar.tsx`，只放图标按钮，文字进 Tooltip |
| 长时间并排使用的面板（Agent、素材、任务） | 页面 | 侧边浮动面板 | 层级 `--z-panel` / `--z-panel-floating`，宽度 `--canvas-panel-width` |
| 右键菜单项 | 节点 / 空白处 | 上下文菜单 | `web/src/components/canvas/canvas-context-menu.tsx`、`web/src/pages/canvas/canvas-project-context-menu.tsx` |

找不到合适的位置时，答案通常是「不加」，或者把它并入已有的菜单。顶栏和 Dock 已经很满，新增一个常驻按钮要拿掉一个。

## 2. 六条硬合同

### 2.1 主题归属：画布主题不是工作台主题

- 画布有独立的明暗主题，默认黑色（`DEFAULT_CANVAS_COLOR_THEME = "dark"`），存在 `yingce:canvas-theme`，由 `web/src/stores/canvas/use-canvas-theme-store.ts` 管理。
- 进入编辑器时 `useCanvasThemeScope()` 把 `active` 置为真，`useActiveTheme()` 返回画布主题，`AppProviders` 和 body 上的 AntD 浮层都跟着切；离开编辑器自动恢复工作台主题。
- 画布内取色只有两个来源：`canvasThemes[colorTheme]` 对象（组件里叫 `theme`），或由它桥接出来的 CSS 变量（`canvasDockStyle(theme)` 写入的 `--dock-*`，提示词面板写入的 `--cn-text` 等）。节点、工具条、连线**不用** `--user-*`、`--library-*` 和全局卡片表面。
- 改画布主题绝不能写工作台偏好，反之亦然。合同测试：`web/test/canvas-theme-isolation.test.ts`。

### 2.2 层级：只用梯子上的 token

层级全部来自 `web/src/styles/globals.css` 的 `--z-*`，完整表见 `references/contracts.md` §1。最常用的几级：节点 `--z-node`(10) / 激活节点 `--z-node-active`(20) → 浮动工具栏 `--z-toolbar`(30) → 节点工具条 `--z-node-toolbar`(50) → 侧边面板 `--z-panel`(60) → 浮动面板 `--z-panel-floating`(80) → 最后交互的画布浮层 `--z-canvas-overlay-active`(150) → 弹出层 `--z-popover`(200)。节点内部用 `--node-z-content` / `--node-z-overlay` / `--node-z-handle`。

不写 `z-[1200]`、`zIndex: 999` 这类字面值。当前代码里还有少量字面值（见 `skills/SKILLS.md` §5 已知偏差），新代码不要模仿。

### 2.3 事件边界：浮在画布上的东西必须声明自己

视口用两个选择器决定一次滚轮 / 指针事件归谁（原文见 `references/contracts.md` §2）：

- 浮在画布上的任何 UI（面板、工具条、菜单宿主、悬浮卡）根节点加 `data-canvas-no-zoom`。不加的后果：在面板上滚轮会缩放画布，在面板上按下会开始框选或拖动画布。
- 面板里可以滚动的区域加 `data-canvas-wheel-scroll`。
- 写在节点内部、需要「悬停稳定后才接管滚轮」的文本区（分镜脚本、长文本）加 `data-canvas-wheel-intent`，交给滚轮所有权算法（§2.4）。
- 节点内部的可点按钮在 `onPointerDown` / `onMouseDown` 里 `stopPropagation`，否则点按钮会同时选中或拖动节点。参考 `canvas-node-status-content.tsx` 的 `stopCanvasPointer`。
- AntD 浮层（`.ant-modal`、`.ant-popover`、`.ant-dropdown`、`.ant-select-dropdown`、`.ant-picker-dropdown`）已经在忽略列表里；自己写的 portal 浮层没有，要自己加 `data-canvas-no-zoom`。

### 2.4 滚轮所有权：先到先得，悬停稳定才转交

`web/src/lib/canvas/canvas-wheel-intent.ts`：

- 一次滚轮手势（相邻事件间隔 < 180ms）开始时就决定归属，中途经过文本区不改主人。快速平移画布时划过节点里的文字，画布不会突然停下。
- 指针在同一个文本区上停留 ≥ 160ms 且移动 ≤ 6px，下一次手势才交给文本区滚动。
- 横向滚动和带修饰键（Ctrl / ⌘）的滚轮总能拿回画布。
- 弹窗、菜单、列表框（`role="dialog"`、`role="menu"`、`role="listbox"`）和 `input`、`select` 永远不参与。
- 门户出去的提示词编辑器要调用 `observeCanvasEditorWheelZoom`，否则 Ctrl+滚轮会缩放整个网页。

合同测试：`web/test/canvas-wheel-intent.test.ts`、`web/test/canvas-wheel.test.ts`。

### 2.5 浮层避让：用测量值，不用猜

- 节点面板（提示词面板、图片编辑器）位置由 `placeCanvasNodePanel` 计算：空间够时锚在节点下方并跟随拖动和缩放；空间不够时贴进可用区域；底部不压住 Dock，右侧不进 Agent 栏。
- 节点工具条用 `avoidCanvasNodePanels` 躲开已打开的面板。面板打开、改尺寸、关闭时用 `updateCanvasNodePanelBounds` 上报边界，订阅方只在边界真的变化时收到通知。
- 顶部工具条按**实测宽度**折叠（`canvasToolbarLayoutLevel`），内容放得下才展开，避免在临界宽度来回跳。
- 以上函数都在 `web/src/lib/canvas/canvas-overlay-layout.ts`，合同测试 `web/test/canvas-overlay-layout.test.ts`（覆盖 1366px 视口、窄矮视口、底边编辑器、拖动跟随）。
- 新浮层一律在这套函数上计算位置，不要写死 `top: 64px` 或 `right: 360px`。顶栏下方的偏移用 `--canvas-topbar-offset`，Dock 弹出面板用 `--canvas-dock-popover-offset`，安全边距用 `--canvas-inset-x` / `--canvas-inset-y`。

### 2.6 性能：节点分三档渲染

`web/src/lib/canvas/canvas-node-lod.ts`：

- `shell`：不在视口、也不在入场区，只画外壳。
- `preview`：总览缩放（缩放比 < 0.23 进入，> 0.27 才退出，带回差避免在边界反复挂载编辑器），或刚进入视口的入场区。
- `full`：在视口内且非总览，或正在编辑。
- 新节点类型必须支持 `shell` / `preview` 两档：不挂载编辑器、播放器和大图解码。已解码的图片在档位切换时保留（`canvas-node-content.tsx`）。
- 浮在画布上的面板，DOM 规模会拖慢画布本身：画布每帧变换时，浏览器要连带处理同一页面上的所有节点。长列表（批量表格、Agent 对话）必须窗口化或分段挂载：批量表格超过 `BATCH_VIRTUAL_ROW_THRESHOLD`（20 行）启用窗口化，Agent 对话只渲染最近 80 条。验收时在 DevTools 里数一次 `document.querySelectorAll("*").length`，面板打开前后差距应在几千以内。
- 入场动画只在本次挂载期间看到「加载中 → 成功」时播放，刷新、滚回视口、切档位重新挂载的已完成节点保持静止。

## 3. 历史缺陷 → 规则

这些规则都来自真实修复。想绕开某条规则前，先看它防的是什么。

| 修复提交（摘要） | 现象 | 防住它的规则 |
| --- | --- | --- |
| 节点拖动 - 未越过拖动阈值前不预览位移 | 点一下节点左右抖 | 指针移动超过阈值（`use-canvas-selection-controller.ts` 里按屏幕 4px 换算）前不预览位移、不做吸附 |
| 新建菜单悬停不跳动 | 悬停时菜单项位移 | 行高固定（40px），说明文字统一放在底部说明区；悬停只换高亮和说明，列表不移动 |
| 演示站反馈 - 小影遮挡、弹层贴边 | 浮层贴住视口边、挡住内容 | §2.5，`CanvasToolPopover` 留 12px 边距 |
| 画布顶栏 - 提示改走浮层避免被生成面板遮挡 | 顶栏提示被面板盖住 | z-index 只在同一个层叠上下文里比较。顶栏本身在 `--z-toolbar` 上，行内提示写多高都越不过 `--z-panel-floating` 的面板；提示必须 portal 出去 |
| 画布交互 - 修复移动浮层定位 | 节点移动后菜单停在原地 | 一次性菜单用 `CanvasAnchoredDropdown`，锚点一动就关 |
| 放大编辑 - 修复资产引用菜单焦点和点击 | 弹窗里的 @ 菜单点不中 | 弹窗里的浮层用 `fixed` + `--z-tooltip`，并留在弹窗的焦点与指针边界内 |
| 后台减少动态效果 - Select 下拉停留在屏幕外 | 关闭动效后浮层不归位 | `transition` 只写具体属性，不写 `all`；否则浮层根节点的 `left/top` 也被过渡 |
| 视频节点 - 签名地址续签时不再重建播放器 | 视频播放中途重载 | 播放器身份是节点媒体的 `storageKey`，不是签名地址；续签只换 `src` |
| 画布生成 - 任务终态后耗时继续增长 | 失败后计时还在走 | 终态以完成时刻冻结耗时、停止订阅秒表；就地重新生成要清掉旧任务的计时字段 |
| 画布连线 - 节点拖动时双层路径叠加残影 | 拖动时连线重影 | 拖动预览由图形层逐帧绘制，拖动期间隐藏静态 SVG 描边，同一时刻只有一套位置 |
| 保护生成中与失败节点画幅尺寸 | 未完成节点被拉成 16:9 | 生成中、失败、空节点按目标比例保持占位框尺寸（`web/src/lib/canvas/canvas-node-size.ts`），状态内容不改变节点尺寸 |
| 长对话下 Agent 面板拖动画布卡顿 | 5,645 条消息让整页 DOM 从约 1 千涨到 6.1 万节点，拖画布掉帧 | §2.6：只渲染最近 80 条，更早的折叠为按钮，消息项加 `content-visibility`；修复后整页 DOM 4,540 |
| 作品载入 - 已生成作品反复载入失败 | 作品已生成却显示失败 | 失败按下一步动作分类（`references/anatomy.md` §2），作品已生成不建议重新生成 |
| 提示浮层 - 修复键盘触发 | Tooltip 只能鼠标触发 | 工具条按钮聚焦也显示提示，图标按钮必须有 `aria-label` |

查看完整历史：`git log --no-merges --pretty='%h %s' | grep '^.\{9\}fix(canvas)'`。

## 4. 外观要点

- **节点不是卡片**：节点表面用 `theme.node.fill`，默认 1px 透明描边占位，选中才换 `theme.node.activeStroke`，不靠阴影和颜色区分状态。标题在节点外上方（11px），不在节点内部做标题栏。
- **工具条按钮默认透明**：Dock 和节点工具条的按钮只有悬停（`--dock-command-hover`）和激活（`--dock-command-active`）有表面；边框和阴影只属于外层面板。
- **类型色只做识别**：`--cn-type-*` 只用在类型图标、类型徽标和小地图色块，不做节点大面积底色。
- **画布上的危险色**用 `theme.accent.danger`，不用全局 `--status-error`，因为画布主题可能和工作台主题不同。
- **画布内的弹窗**仍用 `AppModal`，弹窗打开时 `useActiveTheme()` 已经切到画布主题，不需要再传颜色。
- 详细取值见 `references/theme.md`，节点结构见 `references/anatomy.md`。

## 5. 禁止

- 在画布组件里写十六进制颜色表达界面状态（媒体内容本身的颜色，例如打光预设的色卡，不在此列）。
- 新写 `antd` 的 `Modal`；用 `AppModal`。
- 自己写 portal 浮层却不加 `data-canvas-no-zoom`。
- 用 `position: fixed` + 写死偏移放浮层，不走 `canvas-overlay-layout.ts`。
- 在组件里按工具 ID 排节点工具条顺序；顺序和分组只在注册表里决定。
- 失败态只回显错误原文，不告诉用户下一步。
- 节点状态内容改变节点尺寸。
- 修改画布主题时写入工作台主题，或反过来。

## 6. 怎么验证

- 合同测试（改哪块跑哪块）：`cd web && bun test test/canvas-theme-isolation.test.ts test/canvas-wheel-intent.test.ts test/canvas-wheel.test.ts test/canvas-overlay-layout.test.ts test/canvas-node-toolbar.test.ts`；连线改动加跑 `bun test test/canvas-connection-*.test.ts*`。
- 浏览器：在真实 `/canvas/:id` 上分别验证明暗两种**画布**主题（与工作台主题交叉：工作台亮 + 画布暗、工作台暗 + 画布亮）；在面板、菜单、节点文字上各滚一次滚轮，确认画布不误缩放；在 1366×768 和 1920×1080 下打开提示词面板，确认不压 Dock、不进 Agent 栏。
- 交付自检的通用部分见 `skills/yingce-page-review/SKILL.md`。
