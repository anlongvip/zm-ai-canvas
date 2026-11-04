# 影策设计 Skills 地图

这里是影策（`ddcat-ai/open-ai-canvas`）的产品设计、UI、配色、文案、画布组件与交付自检规范，写给**任何 AI 编码助手**和协作者（Claude、Kiro、Cursor、Codex、GPT、DeepSeek 等）。目标：不管谁来写页面，产出的都是同一个产品，而不是「把一堆功能平铺在页面上」。

每个 Skill 是一个目录，入口 `SKILL.md`，文件头 `name` / `description` 采用通用 Skill 格式，可被支持 Skills 的工具直接加载；不支持的工具把对应 `SKILL.md` 当提示词读入即可。每个 `SKILL.md` 开头都有本目录的「地图」，写明里面每个文件回答什么问题。

## 1. 按任务找

| 你要做的事 | 先读 | 再读 | 交付前 |
| --- | --- | --- | --- |
| 新建或大改一个用户页面 | `yingce-product-design`（§1 四件事 + `references/patterns.md` §0 选范式） | `yingce-ui-system`、`yingce-ui-copy` | `yingce-page-review` |
| 给画布加节点、工具、面板、菜单、弹窗 | `yingce-canvas-ui` | `yingce-ui-system`（只看组件与文案部分） | `yingce-canvas-ui` §6 + `yingce-page-review` |
| 改画布的颜色、层级、滚轮、拖动 | `yingce-canvas-ui/references/contracts.md` | `references/theme.md` | 跑 §6 里的合同测试 |
| 后台（`/admin`）页面 | `yingce-product-design`（§6）→ `references/patterns.md` §10–12 | `yingce-ui-system`（§9） | `yingce-page-review` + 后台视觉基线 |
| 改一个现有组件、弹窗、面板的样子 | `yingce-ui-system` + `references/components.md` | `yingce-color` | `yingce-page-review` |
| 写或改界面文字 | `yingce-ui-copy` | — | — |
| 选颜色、加状态色、做暗色 | `yingce-color` | `yingce-ui-system` | — |

## 2. 按代码位置找

改到下面这些路径时，对应的规范是：

| 改动路径 | 规范 |
| --- | --- |
| `web/src/components/canvas/`、`web/src/pages/canvas/`、`web/src/lib/canvas/`、`web/src/lib/canvas-theme.ts`、`web/src/stores/canvas/` | `yingce-canvas-ui` |
| `web/src/pages/admin/`（含 `theme/admin-tokens.css`） | `yingce-product-design` §6、`patterns.md` §10–12、`yingce-ui-system` §9、`docs/design/admin-ui-design-standard.md` |
| `web/src/pages/create/` | `patterns.md` §3 |
| `web/src/pages/workbench/`、`web/src/pages/ecommerce/` | `patterns.md` §4 |
| `web/src/pages/projects/` | `patterns.md` §5 |
| `web/src/pages/assets/`、`skills/`、`plugins/`、`inspirations/` | `patterns.md` §6 |
| `web/src/pages/tasks/` | `patterns.md` §7 |
| `web/src/pages/settings/` | `patterns.md` §8 |
| `web/src/pages/auth/` | `patterns.md` §9 |
| `web/src/layouts/`、`web/src/components/layout/` | `patterns.md` §1、`yingce-ui-system` §4 |
| `web/src/styles/globals.css`、`web/src/styles/button-tokens.css`、`web/src/lib/app-theme.ts` | `yingce-ui-system` §1–3、`yingce-color` |
| `web/src/components/ui/` | `yingce-ui-system/references/components.md`、`web/src/components/ui/README.md` |

## 3. 目录

```
skills/
├── SKILLS.md                         本文件：地图、校验方式、已知偏差
├── yingce-product-design/            产品设计：页面为谁、解决什么、怎么组织
│   ├── SKILL.md                      四件事、组织方法、信息层级、反面清单、后台、改造步骤
│   └── references/patterns.md        §0 选范式 → 15 个范式（外壳、画布、/create、工作台、短剧、内容库、
│                                     任务、设置、登录、后台列表/设置/概览、局部范式、弹窗、流程卡）
├── yingce-canvas-ui/                 画布组件：主题归属、层级、事件边界、滚轮、浮层、LOD、历史缺陷
│   ├── SKILL.md                      放哪里、六条硬合同、缺陷 → 规则、外观、禁止、验证
│   └── references/
│       ├── anatomy.md                节点结构与状态、失败分类、工具条、提示词面板、连线、浮动控件、菜单、弹窗
│       ├── contracts.md              层级梯子、事件选择器、滚轮常数、拖动、键盘焦点、定位函数、渲染档位
│       └── theme.md                  canvasThemes 明暗取值与桥接变量
├── yingce-ui-system/                 UI 系统：token、表面、按钮合同、版面、状态、动效、组件、CSS 写法
│   ├── SKILL.md
│   └── references/components.md      可用组件清单（用户端、后台、画布）
├── yingce-color/                     配色：颜色角色、明暗取值、使用比例与禁忌
│   └── SKILL.md
├── yingce-ui-copy/                   界面文案：各位置怎么写、反例、用词表
│   └── SKILL.md
└── yingce-page-review/               交付自检：验证环境、必截画面、检查清单、交付说明
    └── SKILL.md
```

## 4. 什么能证明规范是对的

规范本身不算证据。能证明规范和结果正确的，是下面这些不依赖写规范的人的东西：

| 证据 | 证明什么 | 怎么用 |
| --- | --- | --- |
| `bun scripts/check-skills.mjs` | 规范里写的每个路径、CSS 变量、组件名在仓库中真实存在 | 改规范或重命名代码后跑；失败说明规范与代码漂移 |
| 合同测试（`web/test/canvas-*.test.ts` 等） | 画布主题隔离、滚轮所有权、浮层避让、工具条分组这些规则被代码执行 | 规范里每条硬合同都写了对应测试，改动后跑 |
| `bun scripts/verify-before-push.mjs` | 格式、类型、lint、单测、后端测试与 CI 同口径 | 推送前必跑 |
| 真实路由截图（亮 / 暗 × 多宽度） | 页面在用户看到的条件下成立 | 按 `yingce-page-review` 流程 |
| 后台视觉基线 `docs/design/admin-ui-visual-regression-baseline.md` | 18 个后台入口在亮色 1600×1000、暗色 1278×900 下无横向溢出、边界一致 | 改后台壳层或共享组件时对照 |
| `git log` 里的 `fix(canvas|web|ui|admin)` 提交 | 规则防的是真实发生过的缺陷 | `yingce-canvas-ui` §3 列出了对应关系 |
| 用户反馈与 Issue | 规范有没有解决用户真正遇到的问题 | 新问题先找对应规则；找不到就是规范缺口 |

## 5. 已知偏差（代码与规范不一致，新代码不要模仿）

以下是写这份规范时实测到的现状，数字可用 §4 的命令复查。修掉一项就从表里删掉。

| 偏差 | 现状 | 规范要求 |
| --- | --- | --- |
| 画布里直接用 antd `Modal` | `web/src/components/canvas/` 下 16 个文件 | 用 `AppModal` |
| 画布里的 z-index 字面值 | `components/canvas`、`pages/canvas` 共 6 处（如全景图预览 `z-[1200]`） | 用 `--z-*` |
| Agent 对话面板里的十六进制状态色 | `canvas-cloud-agent-chat-ui.tsx` 13 处 | 用状态 token 或 `theme.accent.*` |
| `docs/design/canvas-floating-controls.mdx` 写的「右下工作模式切换」 | 代码里 `workspaceMode` 固定为 `professional`，没有该控件 | 以 `yingce-canvas-ui/references/anatomy.md` §6 为准 |
| `docs/design/workspace-shell-design.mdx` 写的侧栏 260px | `--workspace-sidebar-width: 250px` | 以 token 为准 |
| 后台「电商知识与技能」用页头标签合并两个页面 | `ecommerce-management-page.tsx` | 后台规范要求每个页面独立侧栏入口 |
| `AGENTS.md` 引用的 「docs/ui-design-system.md」，以及 「docs/content/docs/canvas/canvas-node-manual.mdx」 | 两者都被 `.gitignore` 忽略，只在个别开发机上存在，克隆仓库后读不到 | Skill 只引用 Git 跟踪的文件；`check-skills.mjs` 按 `git ls-files` 校验 |
| 仓库里同时存在 `web/src/styles/globals-1.css` | 没有任何文件引用，内容与 `globals.css` 部分重复 | 只改 `globals.css` |

## 6. 给工具配置

- **Claude Code / Kiro / 其他支持 Skills 的工具**：把 `skills/` 下的目录链接到该工具的 skills 目录（例如 `ln -s ../../skills/yingce-canvas-ui .claude/skills/yingce-canvas-ui`），或在会话开头让它读取对应 `SKILL.md`。
- **Cursor / Copilot / 通用聊天模型**：任务开头附上「先读 `skills/SKILLS.md`，按 §1 或 §2 读取对应 SKILL.md」。
- 根目录 `AGENTS.md` 已指向本文件；改 UI 的任务默认先读这里。

## 7. 维护

- 规范与代码不一致时**以代码为准**，修正规范；如果代码是错的，把它写进 §5，并在修复后删掉。
- 改了 token、组件、页面结构，同一个提交里更新对应 Skill，并跑 `bun scripts/check-skills.mjs`。
- 每条规则要么有真实例子（文件路径），要么有它防住的缺陷（提交或 Issue）。两样都给不出的规则不要写。
- 写反例时写清楚「错在哪、应该怎么做」，不要只写「不要这样」。
