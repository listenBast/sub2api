param(
    [string]$UpstreamRepository = 'https://github.com/Wei-Shaw/sub2api.git',
    [string]$OriginRepository = 'https://github.com/listenBast/sub2api.git'
)

$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

if (-not (Test-Path -LiteralPath '.git')) {
    throw 'This directory is not a Git repository. Initialize the fork first.'
}

if (git status --porcelain) {
    throw 'The worktree is not clean. Commit or stash changes before syncing upstream.'
}

if (-not (git remote get-url origin 2>$null)) {
    git remote add origin $OriginRepository
}
if (-not (git remote get-url upstream 2>$null)) {
    git remote add upstream $UpstreamRepository
}

git fetch origin main --tags
git fetch upstream main --tags

git merge-base --is-ancestor upstream/main origin/main
if ($LASTEXITCODE -eq 0) {
    Write-Host 'The main branch already contains the latest upstream changes.'
    exit 0
}

$versionFile = Join-Path $repoRoot 'backend\cmd\server\VERSION'
$forkVersion = (Get-Content -LiteralPath $versionFile -Raw).Trim()
$branch = 'sync/upstream-' + (Get-Date -Format 'yyyyMMdd-HHmmss')

git switch --create $branch origin/main
git merge --no-ff --no-commit upstream/main
if ($LASTEXITCODE -ne 0) {
    Write-Host 'Merge conflicts found. Resolve them while preserving fork customizations, then commit the sync branch.'
    exit 1
}

Set-Content -LiteralPath $versionFile -Value $forkVersion -Encoding ascii
git add --all
git commit -m "merge: sync upstream main into $branch"
git push --set-upstream origin $branch

$compareURL = "https://github.com/listenBast/sub2api/compare/main...$branch?expand=1"
if (Get-Command gh -ErrorAction SilentlyContinue) {
    gh pr create --repo listenBast/sub2api --base main --head $branch --title "Sync upstream Wei-Shaw/sub2api" --body "Automated upstream merge. Review team mode, balance transactions, update sources, and VERSION before merging."
} else {
    Write-Host "The sync branch was pushed. Create a pull request at: $compareURL"
}
