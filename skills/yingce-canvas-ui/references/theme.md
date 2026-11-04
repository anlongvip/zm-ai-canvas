# 画布主题取值

来源：`web/src/lib/canvas-theme.ts` 的 `canvasThemes`。下表是当前取值，改值时以源码为准并同步本表。组件里用 `canvasThemes[colorTheme]`（通常叫 `theme`）读取；要给 CSS 用，就像 `canvasDockStyle(theme)` 和提示词面板那样在根节点写成 CSS 变量。

用户可以自定义画布底色、明亮度、网格颜色和强度（`web/src/lib/canvas/canvas-appearance.ts`），但节点、工具浮层、文字、边框和阴影始终用下表，不随自定义变化。

## 画布与连线

| 字段 | 亮 | 暗 | 用途 |
| --- | --- | --- | --- |
| `canvas.background` | `#f0f0f0` | `#000000` | 创作空间底色，必须与节点表面有稳定明度差 |
| `canvas.dot` / `canvas.line` | `rgba(0,0,0,.80)` | `rgba(255,255,255,.32)` | 点阵 / 线稿网格 |
| `canvas.selectionFill` | `rgba(17,17,17,.10)` | `rgba(255,255,255,.12)` | 框选区域 |
| `connection.stroke` | `#a3a3a3` | `#737373` | 普通连线 |
| `connection.beam` | `#404040` | `#e5e5e5` | 连线流光 |

## 节点

| 字段 | 亮 | 暗 | 用途 |
| --- | --- | --- | --- |
| `node.fill` | `#ffffff` | `#181818` | 节点主体 |
| `node.panel` | `#ffffff` | `#141414` | 节点内子表面、提示词面板 |
| `node.stroke` | `#e2e4e8` | `rgba(255,255,255,.12)` | 节点内分隔 |
| `node.edge` | `rgba(15,23,42,.16)` | `rgba(255,255,255,.18)` | 面板与画布之间的低对比边界 |
| `node.shadow` | `0 6px 18px rgba(15,23,42,.08)` | `0 8px 24px rgba(0,0,0,.34)` | 静止层级 |
| `node.hoverShadow` | `0 10px 24px rgba(15,23,42,.12)` | `0 12px 30px rgba(0,0,0,.46)` | 悬停层级 |
| `node.activeStroke` | `#111827` | `#f1f1f1` | 选中描边 |
| `node.text` / `node.muted` / `node.faint` | `#111827` / `#6b7280` / `#9ca3af` | `#ededed` / `#a3a3a3` / `#666666` | 正文 / 辅助 / 占位 |
| `node.label` | `#4b5563` | `#a3a3a3` | 节点外标题 |
| `node.placeholder` | `#9ca3af` | `#737373` | 输入占位 |
| `node.agentUserMessage` | `#edf6ff` | `#182b40` | Agent 面板里用户消息的底色 |

## 背板（frame）

| 字段 | 亮 | 暗 |
| --- | --- | --- |
| `frame.fill` | `rgba(17,24,39,.025)` | `rgba(255,255,255,.025)` |
| `frame.stroke` | `rgba(17,24,39,.18)` | `rgba(190,198,210,.15)` |
| `frame.activeFill` | `rgba(17,17,17,.05)` | `rgba(255,255,255,.08)` |
| `frame.activeStroke` | `#171717` | `#f5f5f5` |
| `frame.preview` | `rgba(255,255,255,.82)` | `rgba(20,20,20,.94)` |

## 工具条与空间表面

| 字段 | 亮 | 暗 | 用途 |
| --- | --- | --- | --- |
| `toolbar.panel` | `rgba(255,255,255,.94)` | `rgba(20,20,20,.97)` | 工具条面板 |
| `toolbar.border` | `rgba(17,24,39,.10)` | `rgba(255,255,255,.1)` | 面板边界、Tooltip 边界 |
| `toolbar.item` | `#4b5563` | `#d4d4d4` | 按钮图标与文字 |
| `toolbar.itemHover` | `rgba(17,24,39,.06)` | `rgba(255,255,255,.07)` | 悬停表面（`--dock-command-hover`） |
| `toolbar.activeBg` | `rgba(17,24,39,.10)` | `rgba(255,255,255,.10)` | 激活表面（`--dock-command-active`） |
| `toolbar.activeText` | `#111827` | `#f5f6f8` | 激活文字 |
| `spatial.surface` | `rgba(255,255,255,.72)` | `rgba(22,22,22,.82)` | 浮动面板 |
| `spatial.elevated` | `rgba(255,255,255,.94)` | `rgba(15,15,15,.97)` | 更高一层（Tooltip 底） |
| `spatial.dropzone` | `rgba(248,250,252,.78)` | `rgba(8,8,8,.9)` | 拖入素材时的落点 |
| `spatial.shadow` | `rgba(15,23,42,.18)` | `rgba(0,0,0,.6)` | 浮层阴影色 |

## 时间线

| 字段 | 亮 | 暗 |
| --- | --- | --- |
| `timeline.trackFill` | `#f6f8fb` | `#101114` |
| `timeline.clipVideo` | `rgba(79,110,232,.16)` | `rgba(96,126,234,.22)` |
| `timeline.clipAudio` | `rgba(16,185,129,.14)` | `rgba(16,185,129,.20)` |
| `timeline.clipSubtitle` | `rgba(245,158,11,.16)` | `rgba(245,158,11,.20)` |
| `timeline.playhead` / `clipSelectedBorder` | `#171717` | `#f5f5f5` |

## 强调色

| 字段 | 亮 | 暗 | 用途 |
| --- | --- | --- | --- |
| `accent.primary` | `#171717` | `#f5f5f5` | 选中连线、主动作；画布的强调色是反色墨色，不是蓝色 |
| `accent.primarySoft` | `rgba(17,17,17,.10)` | `rgba(255,255,255,.11)` | 强调淡底 |
| `accent.onPrimary` | `#ffffff` | `#131313` | 强调色上的文字 |
| `accent.danger` | `#f87171` | `#fb7185` | 删除、失败（画布内用它，不用全局 `--status-error`） |

## 桥接出来的 CSS 变量

| 变量 | 来源 | 写入位置 |
| --- | --- | --- |
| `--dock-command-bg` / `--dock-command-hover` / `--dock-command-active` / `--dock-command-active-text` / `--dock-command-danger` | `spatial.surface` / `toolbar.itemHover` / `toolbar.activeBg` / `toolbar.activeText` / `accent.danger` | `canvasDockStyle(theme)` |
| `--dock-tooltip-bg` / `--dock-tooltip-border` | `spatial.elevated` / `toolbar.border` | 同上 |
| `--dock-switch-track` / `--dock-switch-thumb` / `--dock-switch-thumb-text` | `toolbar.itemHover` / `node.panel` / `toolbar.activeText` | 同上 |
| `--dock-surface` / `--dock-border` | `--cn-spatial-surface` / 透明 | `globals.css` |
| `--cn-text` 等 | `node.text` 等 | 提示词面板根节点 |

`--cn-*` 在 `globals.css` 里也有一份按工作台 `.dark` 定义的默认值。编辑器里 `useActiveTheme()` 让全局 `.dark` 跟随画布主题，所以两份一致；但在画布组件里读颜色仍优先用 `theme` 对象，避免组件被拿到画布外使用时颜色和画布对不上。
