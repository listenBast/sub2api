# Sub2API 二开版本维护说明

本项目使用以下两个远端：

- `origin`: `https://github.com/listenBast/sub2api.git`，用于发布和在线更新。
- `upstream`: `https://github.com/Wei-Shaw/sub2api.git`，只用于获取开源项目更新。

## 版本规则

本项目从 `v0.1.0` 开始独立编号。上游版本号不直接作为本项目版本号。

每次发布依次使用 `v0.1.1`、`v0.1.2`、`v0.1.3`。源码文件
`backend/cmd/server/VERSION` 不包含 `v` 前缀，例如填写 `0.1.1`。

## 最快同步上游的方法

仓库每周一会运行 `.github/workflows/upstream-sync.yml`。也可以在 GitHub 的
`Actions -> Sync upstream -> Run workflow` 手动执行。工作流会：

1. 拉取 `Wei-Shaw/sub2api` 的最新 `main`。
2. 创建独立的 `sync/upstream-*` 分支。
3. 合并成功后创建 PR，不直接修改你的 `main`。
4. 始终保留当前二开版本号，避免被上游 `VERSION` 覆盖。

如果 GitHub Actions 提示冲突，在 Windows 本机执行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\sync-upstream.ps1
```

解决冲突时重点保留：团队余额事务、团队管理界面、`listenBast/sub2api` 更新源、
`ghcr.io/listenbast/sub2api` 镜像地址和本项目的 `VERSION`。

## 合并后发布自己的版本

同步 PR 通过测试并合并到 `main` 后：

```powershell
Set-Content backend\cmd\server\VERSION 0.1.1 -Encoding ascii
git add backend\cmd\server\VERSION
git commit -m "release: v0.1.1"
git tag -a v0.1.1 -m "v0.1.1"
git push origin main
git push origin v0.1.1
```

推送标签会触发 `.github/workflows/release.yml`，在你自己的 GitHub Releases 中生成：

- Linux、Windows、macOS 安装包。
- `checksums.txt`。
- `ghcr.io/listenbast/sub2api:0.1.1` Docker 镜像。

发布完成后，Sub2API 左上角“版本更新”会检查
`listenBast/sub2api` 的最新 Release。点击更新或回退时，也只会读取该仓库的版本和安装包。

数据库迁移通常是单向的。执行版本更新前仍应先做数据库备份；程序回退不能自动撤销已经执行的数据库结构迁移。
