param(
    [string]$Distro = "zm-ai-canvas-ci",
    [int]$Jobs = 4
)
$ErrorActionPreference = "Stop"
$repo = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$status = git -C $repo status --porcelain
if ($LASTEXITCODE -ne 0) { throw "无法读取 Git 工作区状态" }
if ($status) { throw "工作区必须干净。请先提交要验证的改动，再运行 Linux 验证。" }
$head = (git -C $repo rev-parse HEAD).Trim()
if (-not $head) { throw "无法读取当前提交" }

$cache = Join-Path $repo ".local/cache"
New-Item -ItemType Directory -Force -Path $cache | Out-Null
$bundle = Join-Path $cache "linux-verify-$head.bundle"
$logPath = Join-Path $cache "linux-verify.log"
$bundlePath = "/mnt/$($repo.Substring(0,1).ToLower())/" + ($repo.Substring(3) -replace "\\", "/") + "/.local/cache/linux-verify-$head.bundle"

git -C $repo bundle create $bundle HEAD origin/main
if ($LASTEXITCODE -ne 0) { throw "创建临时验证 bundle 失败" }

# 每次使用独立副本，避免污染长期 WSL 仓库和历史 worktree 元数据。
$bash = @"
set -euo pipefail
source /opt/canvas-ci/env.sh
root=/opt/canvas-ci/verify/$head
# 只保留最近一份历史副本：每份约 1 GB，长期累积会把 WSL 磁盘撑大。
ls -1dt /opt/canvas-ci/verify/*/ 2>/dev/null | tail -n +2 | xargs -r rm -rf || true
rm -rf "`$root"
git init -q "`$root"
git -C "`$root" config core.autocrlf false
git -C "`$root" fetch --quiet "$bundlePath" HEAD:refs/heads/verify refs/remotes/origin/main:refs/remotes/origin/main
git -C "`$root" checkout -q verify
git -C "`$root" config status.showUntrackedFiles no
ln -s /opt/canvas-ci/repo/web/node_modules "`$root"/web/node_modules
ln -s /opt/canvas-ci/repo/backend/agent-runtime/pi/node_modules "`$root"/backend/agent-runtime/pi/node_modules
cd "`$root"
bun scripts/verify-before-push.mjs --jobs $Jobs
"@

try {
    $bash | & wsl.exe -d $Distro -- bash -s 2>&1 | Tee-Object -FilePath $logPath
    if ($LASTEXITCODE -ne 0) { throw "Linux 验证失败，日志：$logPath" }
    Write-Output "Linux 验证通过：$head；日志：$logPath"
}
finally {
    Remove-Item -LiteralPath $bundle -Force -ErrorAction SilentlyContinue
}

