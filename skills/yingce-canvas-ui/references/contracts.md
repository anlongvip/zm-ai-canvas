# 画布交互合同

这些合同决定一个事件、一层浮层、一次重绘归谁。每条都写了源码位置和守住它的测试；改动前先跑对应测试，改完再跑一次。

## 1. 层级梯子

全部定义在 `web/src/styles/globals.css`。z-index 只在同一个层叠上下文里比较：父元素建立了上下文（`position` + `z-index`、`transform`、`filter`、`backdrop-filter`、`opacity < 1`），子元素写多高都越不过父元素的兄弟。需要越级的浮层（提示、菜单）一律 portal 到 `document.body`。

| token | 值 | 层 | 放什么 |
| --- | --- | --- | --- |
| `--z-canvas` | 0 | L0 | 画布底 |
| `--z-canvas-overlay` | 5 | L1 | 网格、连线 |
| `--z-node` | 10 | L2 | 未选中节点 |
| `--z-node-active` | 20 | L3 | 选中、拖动中的节点 |
| `--z-toolbar` | 30 | L4 | 顶栏、Dock、视图控制 |
| `--z-node-toolbar` | 50 | L5 | 节点工具条 |
| `--z-panel` | 60 | L6 | 侧边面板、素材托盘 |
| `--z-panel-floating` | 80 | L7 | Agent、助手、活动任务等浮动面板 |
| `--z-modal` | 100 | L8 | 对话框 |
| `--z-modal-overlay` | 110 | L9 | 遮罩 |
| `--z-canvas-overlay-active` | 150 | — | 最后交互的画布浮层（`canvas-overlay-layer.tsx` 提升） |
| `--z-popover` | 200 | L10 | 弹出层 |
| `--z-toast` | 500 | L11 | 通知 |
| `--z-dialog-popover` | 1100 | — | AntD Modal（1000）之上的设置浮层 |
| `--z-tooltip` | 1150 | L12 | 提示；必须压过弹窗和弹窗内浮层 |
| `--z-loader` | 1200 | — | 全屏加载遮罩 |
| `--z-max` | 2000 | L13 | 紧急通知 |

节点内部：`--node-z-content`(30) < `--node-z-overlay`(40) < `--node-z-handle`(50)，只在节点自己的层叠上下文里有效。

## 2. 事件边界

源码：`web/src/components/canvas/canvas-viewport.tsx`。

```ts
const CANVAS_WHEEL_IGNORE_SELECTOR = "input,textarea,select,[contenteditable]:not([contenteditable='false']),[data-canvas-no-zoom],[data-canvas-wheel-scroll],.ant-modal,.ant-popover,.ant-dropdown,.ant-select-dropdown,.ant-picker-dropdown";
const CANVAS_POINTER_IGNORE_SELECTOR = "[data-canvas-no-zoom],[data-connection-create-menu],.ant-modal,.ant-popover,.ant-dropdown,.ant-select-dropdown,.ant-picker-dropdown";
const CANVAS_INTERNAL_DRAG_SELECTOR = "[data-canvas-batch-table]";
```

| 属性 | 加在哪里 | 效果 |
| --- | --- | --- |
| `data-canvas-no-zoom` | 浮在画布上的任何 UI 的根节点 | 滚轮不缩放画布，按下不开始框选和平移；除 ⌘/Ctrl+C、⌘/Ctrl+V 外的画布快捷键不响应 |
| `data-canvas-wheel-scroll` | 浮层内可以滚动的区域 | 滚轮交给该区域滚动 |
| `data-canvas-wheel-intent` | 节点内部的长文本、分镜表 | 进入滚轮所有权算法（§3），悬停稳定后才接管 |
| `data-canvas-node-panel` | 节点面板根节点 | 面板虽然是视口的兄弟，仍算作该视口的一部分（`canvasWheelBelongsToViewport`） |
| `data-connection-create-menu` | 连线创建菜单 | 指针不触发画布操作 |
| `data-canvas-batch-table` | 批量表格 | 内部拖动不当成画布拖动 |
| `data-canvas-resource-mention-menu` | `@` 素材菜单 | 不参与滚轮所有权 |

## 3. 滚轮所有权

源码：`web/src/lib/canvas/canvas-wheel-intent.ts`。测试：`web/test/canvas-wheel-intent.test.ts`、`web/test/canvas-wheel.test.ts`。

| 常数 | 值 | 含义 |
| --- | --- | --- |
| `CANVAS_WHEEL_HOVER_MS` | 160ms | 指针在文本区上停够这么久，下一次手势才归文本区 |
| `CANVAS_WHEEL_GESTURE_IDLE_MS` | 180ms | 相邻滚轮事件间隔小于它算同一次手势，归属不变 |
| `CANVAS_WHEEL_POINTER_SLOP` | 6px | 悬停期间移动超过它重新计时 |
| `WHEEL_ZOOM_DELTA` | 72 | 鼠标滚轮缩放步长（`canvas-viewport.tsx`） |
| `TRACKPAD_PINCH_ZOOM_DELTA` | 24 | 触控板捏合缩放步长 |

行为：
1. 手势开始时决定主人；手势中途经过别的文本区，主人不变（快速平移不会被文字截停）。
2. 主人只能是悬停已稳定的文本区；否则归画布。
3. 横向滚动、带修饰键的滚轮可以把手势拿回画布。
4. `input`、`select` 和弹窗、菜单、列表框永远不参与。
5. Windows 缩放、行模式滚轮和触控板细微移动都要识别为正常滚轮；非法 delta 忽略。
6. 门户出去的编辑器调用 `observeCanvasEditorWheelZoom(container)`，在捕获阶段用非被动监听拦掉 Ctrl/⌘+滚轮，否则整页被浏览器缩放。

## 4. 指针与拖动

- 选区控制：`web/src/pages/canvas/use-canvas-selection-controller.ts`。指针移动超过屏幕 4px（世界坐标 `4 / viewport.k`）才算拖动；之前不预览位移、不做对齐吸附。
- 节点内可点控件在 `onPointerDown` 和 `onMouseDown` 里 `stopPropagation`。
- 尺寸调整：`use-canvas-node-resize`，按下任意位置先结束上一次尺寸动画（`resizeMotion.finishAll()`）。
- 需要连续拖动的控件（滑块、时间线手柄）用 pointer capture，松手或 `pointercancel` 都要释放。
- 时间线吸附阈值 8px（`canvas-timeline-dialog.tsx` 的 `SNAP_THRESHOLD_PX`）。

## 5. 键盘与焦点

源码：`web/src/pages/canvas/use-canvas-keyboard.ts`，快捷键说明 `web/src/pages/canvas/canvas-shortcuts-modal.tsx`（`?` 打开）。

- 焦点在节点工具条或其菜单里：画布快捷键全部不响应。
- 焦点在 `input`、`textarea`、`select`、`contenteditable` 里：缩放快捷键只阻止浏览器默认行为、不缩放画布；其他画布快捷键不响应。
- 焦点在 `data-canvas-no-zoom` 区域：只响应复制和粘贴。
- ⌘/Ctrl+F 在弹窗、下拉、弹出层里交给它们自己；其余情况打开画布搜索，加 Shift 切换专注模式。
- ⌘/Ctrl+S 保存，按住不重复触发。
- 新快捷键：先查 `use-canvas-keyboard.ts` 有没有冲突，再写进快捷键弹窗；不和浏览器、输入法常用键冲突。
- 所有图标按钮有 `aria-label`；Tooltip 在键盘聚焦时也出现。持续开关用 `aria-pressed`，展开面板用 `aria-expanded`。
- 弹窗关闭后焦点回到打开它的按钮。

## 6. 浮层定位

源码：`web/src/lib/canvas/canvas-overlay-layout.ts`。测试：`web/test/canvas-overlay-layout.test.ts`。

| 函数 | 用途 |
| --- | --- |
| `canvasOverlayBounds(size, insets)` | 视口减去安全边距后的可用区 |
| `observeCanvasOverlayBounds(container, onChange)` | 监听可用区变化（窗口、侧栏、Agent 栏） |
| `placeCanvasNodePanel(node, viewport, bounds, size, dragOffset)` | 节点面板位置：宽裕时锚在节点下方并跟随，否则贴入可用区 |
| `updateCanvasNodePanelBounds` / `subscribeCanvasNodePanelBounds` / `canvasNodePanelBounds` | 面板上报、订阅、读取边界；相同边界不重复通知 |
| `avoidCanvasNodePanels(anchor, size, bounds, panels)` | 节点工具条躲开面板 |
| `canvasToolbarLayoutLevel(availableWidth, measuredWidths, current)` | 按实测宽度决定工具条折叠档，内容放得下才展开 |

测试覆盖的场景就是验收场景：1366px 视口下迟到的输入框布局、面板改尺寸和关闭、底边图片编辑器在 Dock 之上且不进工作区侧栏、窄矮视口里的超大编辑器、宽裕视口跟随拖动和缩放、工具条按实测宽度折叠。

弹出层：
- `CanvasToolPopover` 外圈 12px 透明内边距（`TOOL_POPOVER_VIEWPORT_GAP`），`pointer-events: none`，点外圈等于点外部；AntD `align.overflow` 必须同时写 `adjustX/adjustY`（翻转）和 `shiftX/shiftY`（平移），只写一个会替换掉内置档位。
- `CanvasAnchoredDropdown` 用 `useDismissPopupOnAnchorMove`：锚点移动就关闭。
- 浮层根节点不要写 `transition: all`；减少动态效果时它会让 `left/top` 停在过渡中途。

## 7. 渲染档位与挂载

- LOD：`web/src/lib/canvas/canvas-node-lod.ts`，进入总览 `scale < 0.23`，退出总览 `scale >= 0.27`（回差 0.04）。
- 渐进挂载：`web/src/pages/canvas/use-canvas-progressive-node-mount.ts`。
- 批量表格窗口化：`BATCH_VIRTUAL_ROW_THRESHOLD = 20`（`web/src/lib/canvas/canvas-batch-window.ts`）。
- 小地图：可加载缩略图的图片节点 ≤ 24 个才显示缩略图。
- 浮动面板的 DOM 规模计入画布每帧成本；长列表只挂载可见的一段，项加 `content-visibility: auto`。

## 8. 动效

- 尊重 `prefers-reduced-motion`：连线流动虚线停止，面板进入动画关闭或缩短；JS 动画用 `motion/react` 的 `useReducedMotion()`。
- 减少动态效果只关过渡，不改变定位与布局。
- 悬停效果只改颜色和透明度，不改尺寸、边框宽度、字重，否则周边元素位移。
