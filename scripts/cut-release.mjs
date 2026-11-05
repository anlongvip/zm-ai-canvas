#!/usr/bin/env bun
/**
 * 一键发版：VERSION + CHANGELOG + 提交 + 打 tag，默认不推送。
 *
 *   bun scripts/cut-release.mjs                    # 按上一个 tag 自动递增第 4 位
 *   bun scripts/cut-release.mjs --version v1.7.4.0 # 指定版本
 *   bun scripts/cut-release.mjs --dry-run          # 只打印计划，不写文件、不提交
 *   bun scripts/cut-release.mjs --push             # 提交并推送 main 与 tag（触发发布流水线）
 *
 * 规则（与 CI 对齐）：
 *   - VERSION 内容必须与 tag 完全一致，否则 tag 构建的 prepare 步骤失败；
 *   - CHANGELOG.md 必须有 `## vX.Y.Z[.N]` 段落，否则 tag 构建失败；
 *   - 只用正式版本号（不带 -rc / -beta），更新器会跳过 GitHub 上的 prerelease，
 *     带后缀的标签发布后无法在后台检测到。
 */
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const VERSION_FILE = path.join(ROOT, "VERSION");
const CHANGELOG_FILE = path.join(ROOT, "CHANGELOG.md");
const VERSION_PATTERN = /^v\d+\.\d+\.\d+(\.\d+)?$/;

const options = parseArgs(process.argv.slice(2));

function parseArgs(argv) {
    const result = { version: "", dryRun: false, push: false, branch: "main", notes: [] };
    for (let index = 0; index < argv.length; index++) {
        const arg = argv[index];
        const value = () => {
            const next = argv[++index];
            if (!next) fail(`${arg} 需要参数`);
            return next;
        };
        if (arg === "--version") result.version = value();
        else if (arg === "--branch") result.branch = value();
        else if (arg === "--note") result.notes.push(value());
        else if (arg === "--dry-run") result.dryRun = true;
        else if (arg === "--push") result.push = true;
        else if (arg === "-h" || arg === "--help") {
            console.log(readFileSync(fileURLToPath(import.meta.url), "utf8").split("*/")[0]);
            process.exit(0);
        } else fail(`未知参数：${arg}`);
    }
    return result;
}

function fail(message) {
    console.error(`错误：${message}`);
    process.exit(1);
}

function git(args, { allowFailure = false } = {}) {
    try {
        return execFileSync("git", args, { cwd: ROOT, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
    } catch (error) {
        if (allowFailure) return "";
        const detail = (error.stderr || error.message || "").toString().trim();
        fail(`git ${args.join(" ")} 失败：${detail}`);
    }
}

function lastTag() {
    const reachable = git(["tag", "--merged", "HEAD", "--list", "v*", "--sort=-v:refname"], { allowFailure: true })
        .split("\n")
        .map((line) => line.trim())
        .filter(Boolean)[0];
    const highest = git(["tag", "--list", "v*", "--sort=-v:refname"], { allowFailure: true })
        .split("\n")
        .map((line) => line.trim())
        .filter(Boolean)[0];
    if (!reachable) fail("当前历史里没有 v* 标签，无法推断上一个发布版本；请用 --version 显式指定");
    if (highest && compareVersions(highest, reachable) > 0) {
        fail(
            `当前分支落后：标签 ${highest} 不在 HEAD 历史里。先在 ${options.branch} 上 git pull --ff-only 再发版，` +
                "否则新版本会漏掉已发布的内容。",
        );
    }
    return reachable;
}

function nextVersion(base) {
    const parts = base.replace(/^v/, "").split(".").map((item) => Number(item));
    if (parts.length === 3) parts.push(0);
    parts[parts.length - 1] += 1;
    return `v${parts.join(".")}`;
}

const CATEGORY = [
    [/^feat/i, "新增"],
    [/^fix/i, "修复"],
    [/^perf/i, "优化"],
    [/^refactor/i, "重构"],
    [/^docs/i, "文档"],
    [/^test/i, "测试"],
    [/^(build|ci)/i, "构建"],
    [/^(chore|style|revert)/i, "维护"],
];

// 版本号提交、合并提交、测试与基建类提交不进入发布说明，它们不描述用户可见变化。
// scope=release 的提交（发版脚本、CI、文档）都属于基建，一律跳过。
function isNoise(subject) {
    return (
        /^[a-z]+\(release\)/i.test(subject) ||
        /^chore\(release\)/i.test(subject) ||
        /^Merge\b/i.test(subject) ||
        /^ci[:(]/i.test(subject) ||
        /^test[:(]/i.test(subject) ||
        /^docs\(agents\)/i.test(subject) ||
        /版本发布/.test(subject)
    );
}

function bulletFor(subject) {
    const clean = subject.replace(/\s+/g, " ").trim();
    const scoped = clean.match(/^[a-z]+\(([^)]+)\):\s*(.+)$/i);
    const typed = clean.match(/^[a-z]+:\s*(.+)$/i);
    const label = CATEGORY.find(([pattern]) => pattern.test(clean))?.[1] ?? "变更";
    if (scoped) return `- ${label}（${scoped[1]}）：${scoped[2]}`;
    if (typed) return `- ${label}：${typed[1]}`;
    return `- ${label}：${clean}`;
}

function buildEntry(version, baseTag) {
    const subjects = git(["log", "--no-merges", "--format=%s", `${baseTag}..HEAD`])
        .split("\n")
        .map((line) => line.trim())
        .filter(Boolean);
    const bullets = [...new Set(subjects.filter((subject) => !isNoise(subject)).map(bulletFor))].slice(0, 40);
    const upstream = git(["log", "--merges", "--format=%s", `${baseTag}..HEAD`], { allowFailure: true })
        .split("\n")
        .map((line) => line.trim())
        .filter((line) => /upstream|作者/i.test(line));
    const touchesSchema = subjects.some((subject) => /schema|迁移|migration/i.test(subject));
    const lines = [`## ${version}`];
    if (upstream.length > 0) {
        lines.push(`- 上游同步：合并作者主线 ${upstream.length} 次（${baseTag}..HEAD）`);
    }
    for (const note of options.notes) lines.push(`- ${note}`);
    if (bullets.length === 0 && options.notes.length === 0) {
        lines.push("- 维护性更新：无用户可见功能变化。");
    } else {
        lines.push(...bullets);
    }
    if (touchesSchema) {
        lines.push("- 升级注意：本轮涉及数据库 Schema 或迁移，请写明升/降级条件与回退方式。");
    }
    return lines.join("\n");
}

function assertClean() {
    const dirty = git(["status", "--porcelain"]);
    if (dirty) {
        fail(`工作区有未提交改动，先提交或还原：\n${dirty}`);
    }
}

function main() {
    const head = git(["rev-parse", "--abbrev-ref", "HEAD"]);
    const baseTag = lastTag();
    const version = options.version || nextVersion(baseTag);
    if (options.version && !VERSION_PATTERN.test(options.version)) {
        fail(`版本号必须是 vX.Y.Z 或 vX.Y.Z.N（不接受 -rc/-beta 等后缀，更新器会跳过 prerelease）：${options.version}`);
    }
    if (options.version && compareVersions(version, baseTag) <= 0) {
        fail(`${version} 必须大于上一个版本 ${baseTag}`);
    }
    if (git(["rev-parse", "-q", "--verify", `refs/tags/${version}`], { allowFailure: true })) {
        fail(`标签 ${version} 已存在，同一提交不要重复发版`);
    }
    if (!options.dryRun) {
        assertClean();
        if (head !== options.branch) fail(`当前在 ${head}，发版必须在 ${options.branch} 上进行`);
    }

    const currentVersion = readFileSync(VERSION_FILE, "utf8").trim();
    // Windows 检出是 CRLF，比较和插入统一按 LF 处理，写回时再还原原有换行。
    const rawChangelog = readFileSync(CHANGELOG_FILE, "utf8");
    const eol = rawChangelog.includes("\r\n") ? "\r\n" : "\n";
    const changelog = rawChangelog.split("\r\n").join("\n");
    if (!changelog.startsWith("# CHANGELOG")) fail("CHANGELOG.md 必须以 # CHANGELOG 开头");
    // 代理或人工可能已经把 VERSION 与 CHANGELOG 写好并提交，这时只需补一个 tag，避免重复条目。
    const prepared = currentVersion === version && changelog.includes(`\n## ${version}\n`);

    console.log(`上一个版本：${baseTag}`);
    console.log(`当前 VERSION：${currentVersion}`);
    console.log(`本次版本：${version}`);
    console.log(`基线分支：${head}${options.dryRun ? "（dry-run，不写入）" : ""}`);
    console.log("");
    if (prepared) {
        console.log(`VERSION 与 CHANGELOG 已经是 ${version}，跳过改写，直接打 tag。`);
    } else {
        const entry = buildEntry(version, baseTag);
        const marker = changelog.indexOf("\n");
        console.log(entry);
        console.log("");
        if (!options.dryRun) {
            const inserted = `${changelog.slice(0, marker + 1)}\n${entry}\n\n${changelog.slice(marker + 1)}`;
            writeFileSync(VERSION_FILE, `${version}\n`, "utf8");
            writeFileSync(CHANGELOG_FILE, inserted.replace(/\n/g, eol), "utf8");
            const writtenVersion = readFileSync(VERSION_FILE, "utf8").trim();
            if (writtenVersion !== version) fail(`VERSION 写入后为 ${writtenVersion}，与 ${version} 不一致`);
            if (!readFileSync(CHANGELOG_FILE, "utf8").split("\r\n").join("\n").includes(`\n## ${version}\n`)) {
                fail(`CHANGELOG 未包含 ## ${version} 段落`);
            }
            git(["add", "VERSION", "CHANGELOG.md"]);
            git(["commit", "-m", `chore(release): 版本发布 - ${version}`]);
        }
    }

    if (options.dryRun) {
        console.log("dry-run：未修改文件、未提交、未打 tag。");
        return;
    }

    git(["tag", "-a", version, "-m", `影策 ${version}`]);
    console.log(`已打 tag：${version}`);

    if (options.push) {
        git(["push", "origin", "HEAD"]);
        git(["push", "origin", version]);
        console.log("已推送 main 与 tag，发布流水线开始运行。");
    } else {
        console.log(`确认无误后推送：git push origin HEAD && git push origin ${version}`);
    }
}

function compareVersions(left, right) {
    const a = left.replace(/^v/, "").split(".").map((item) => Number(item));
    const b = right.replace(/^v/, "").split(".").map((item) => Number(item));
    for (let index = 0; index < Math.max(a.length, b.length); index++) {
        const delta = (a[index] ?? 0) - (b[index] ?? 0);
        if (delta !== 0) return delta;
    }
    return 0;
}

main();
