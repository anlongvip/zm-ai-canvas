#!/usr/bin/env bun
// 校验 skills/ 里引用的路径、CSS 变量和组件名在仓库中真实存在，防止设计规范与代码漂移。
// 用法：bun scripts/check-skills.mjs
import { execFileSync } from "node:child_process";
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

const root = new URL("..", import.meta.url).pathname;
const skillsDir = join(root, "skills");
const webSrc = join(root, "web/src");

function walk(dir, accept, out = []) {
    for (const name of readdirSync(dir)) {
        if (name === "node_modules" || name.startsWith(".")) continue;
        const full = join(dir, name);
        if (statSync(full).isDirectory()) walk(full, accept, out);
        else if (accept(full)) out.push(full);
    }
    return out;
}

// 只认 Git 跟踪的文件：被 .gitignore 忽略的本地文档，别人克隆仓库后并不存在。
const tracked = new Set(execFileSync("git", ["ls-files"], { cwd: root, encoding: "utf8" }).split("\n").filter(Boolean));
const trackedDirs = new Set();
for (const file of tracked) for (let index = file.indexOf("/"); index !== -1; index = file.indexOf("/", index + 1)) trackedDirs.add(file.slice(0, index));
const isTracked = (path) => tracked.has(path) || trackedDirs.has(path);

const sourceFiles = walk(webSrc, (file) => /\.(css|ts|tsx)$/.test(file)).filter((file) => tracked.has(relative(root, file)));
const sources = sourceFiles.map((file) => readFileSync(file, "utf8")).join("\n");

const definedVars = new Set();
for (const match of sources.matchAll(/(--[a-z0-9][a-z0-9-]*)\s*:/gi)) definedVars.add(match[1]);
for (const match of sources.matchAll(/["'`](--[a-z0-9][a-z0-9-]*)["'`]\s*[:\]]/gi)) definedVars.add(match[1]);
// 运行时拼出的变量（例如 `--cn-type-${kind}`）只登记前缀。
const dynamicVarPrefixes = [...sources.matchAll(/`(--[a-z0-9-]+-)\$\{/gi)].map((match) => match[1]);

const exportedNames = new Set();
for (const match of sources.matchAll(/export\s+(?:default\s+)?(?:async\s+)?(?:function|const|class|type|interface|enum)\s+([A-Za-z0-9_]+)/g)) exportedNames.add(match[1]);
for (const match of sources.matchAll(/export\s*\{([^}]+)\}/g)) for (const part of match[1].split(",")) exportedNames.add(part.trim().split(/\s+as\s+/).pop());

const antdIndex = join(root, "web/node_modules/antd/es/index.d.ts");
const dependenciesInstalled = existsSync(antdIndex);
if (!dependenciesInstalled) console.warn("未安装 web 依赖（缺少 antd 类型），跳过第三方组件名校验；运行 cd web && bun install 后可完整校验。");
const externalNames = new Set(existsSync(antdIndex) ? [...readFileSync(antdIndex, "utf8").matchAll(/default as ([A-Za-z]+)/g)].map((match) => match[1]) : []);
// 源码里从第三方包导入过的名字（React Router、lucide 图标等）也算存在。
for (const match of sources.matchAll(/import\s*(?:type\s*)?\{([^}]+)\}\s*from\s*["']([^"'.@][^"']*|@(?!\/)[^"']+)["']/g)) {
    for (const part of match[1].split(",")) externalNames.add(part.trim().replace(/^type\s+/, "").split(/\s+as\s+/)[0]);
}

const pathRoots = ["web/", "backend/", "docs/", "scripts/", "skills/", ".github/"];
const webRelativeRoots = ["components/", "pages/", "lib/", "stores/", "services/", "styles/", "hooks/", "layouts/"];

function pathExists(raw) {
    let clean = raw.replace(/[:#§].*$/, "").replace(/\/$/, "");
    // `<主题>` 这类占位段只校验它前面的目录。
    if (clean.includes("<")) clean = clean.slice(0, clean.indexOf("<")).replace(/\/$/, "");
    let repoPath;
    if (pathRoots.some((prefix) => clean.startsWith(prefix))) repoPath = clean;
    else if (webRelativeRoots.some((prefix) => clean.startsWith(prefix))) repoPath = `web/src/${clean}`;
    else if (clean.startsWith("@/")) repoPath = `web/src/${clean.slice(2)}`;
    else return true;
    if (/[*{}\s]/.test(repoPath)) return globExists(repoPath);
    if (clean.startsWith("@/")) return ["", ".ts", ".tsx", "/index.ts", "/index.tsx"].some((ext) => isTracked(repoPath + ext));
    return isTracked(repoPath);
}

// 只支持文件名段里的 `*`，足够覆盖 `canvas-node-*-dialog.tsx` 这类写法。
function globExists(pattern) {
    const slash = pattern.lastIndexOf("/");
    const dir = pattern.slice(0, slash);
    if (/[*]/.test(dir) || !trackedDirs.has(dir)) return false;
    const regex = new RegExp(`^${pattern.slice(slash + 1).replace(/[.+?^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*")}$`);
    return [...tracked].some((file) => file.startsWith(`${dir}/`) && !file.slice(dir.length + 1).includes("/") && regex.test(file.slice(dir.length + 1)));
}

function varExists(name) {
    if (name.endsWith("*") || name.endsWith("-")) {
        const prefix = name.replace(/\*$/, "");
        return [...definedVars].some((value) => value.startsWith(prefix)) || dynamicVarPrefixes.some((value) => value.startsWith(prefix));
    }
    return definedVars.has(name) || dynamicVarPrefixes.some((prefix) => name.startsWith(prefix));
}

const problems = [];
const skillFiles = walk(skillsDir, (file) => file.endsWith(".md"));
let checked = 0;
for (const file of skillFiles) {
    const lines = readFileSync(file, "utf8").split("\n");
    let fenced = false;
    lines.forEach((line, index) => {
        if (line.trimStart().startsWith("```")) fenced = !fenced;
        if (fenced) return;
        for (const [, span] of line.matchAll(/`([^`]+)`/g)) {
            const where = `${relative(root, file)}:${index + 1}`;
            // 命令行里的 `--flag` 不是 CSS 变量。
            const isCommand = /^(git|bun|cd|npm|go|grep|gh|\.\/)\s/.test(span.trim());
            for (const [, name] of isCommand ? [] : span.matchAll(/(--[a-z0-9][a-z0-9-]*\*?)/gi)) {
                if (/^--(no-verify|jobs|all|only|port|strictPort|headless|remote|include|write|grid|force)/.test(name)) continue;
                checked += 1;
                if (!varExists(name)) problems.push(`${where}  CSS 变量不存在：${name}`);
            }
            const token = span.trim();
            if (/^(@\/|[a-z.][a-z0-9._-]*\/)[^\s]*$/i.test(token) && (pathRoots.some((prefix) => token.startsWith(prefix)) || webRelativeRoots.some((prefix) => token.startsWith(prefix)) || token.startsWith("@/"))) {
                checked += 1;
                if (!pathExists(token)) problems.push(`${where}  路径不存在：${token}`);
            }
            const keyNames = new Set(["Escape", "Enter", "Shift", "Delete", "Backspace", "Tab", "Space"]);
            if (/^[A-Z][A-Za-z0-9]+$/.test(token) && /[a-z]/.test(token) && token.length > 3 && !keyNames.has(token)) {
                checked += 1;
                if (!exportedNames.has(token) && !externalNames.has(token) && dependenciesInstalled) problems.push(`${where}  组件或类型未导出：${token}`);
            }
        }
    });
}

if (problems.length) {
    console.error(problems.join("\n"));
    console.error(`\nskills 校验失败：${problems.length} 处引用找不到（共检查 ${checked} 处）。`);
    process.exit(1);
}
console.log(`skills 校验通过：${skillFiles.length} 个文件，${checked} 处路径 / 变量 / 组件引用全部存在。`);
