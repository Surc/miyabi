# 上游同步记录：2026-11-09

## 比较基线与范围

- Fork：`Surc/miyabi:master`，`261c656d6bb9257a3d2d69c5a5ffaf8b6ec766ea`。
- Upstream：`ppxb/miyabi:master`，`a208f49659697d8d1ab1b0f413f489286edeb45e`。
- 共同祖先：`f1903459b45dece1498c05c7b309773776c756e9`。Fork 有 46 个独有提交，上游有 3 个独有提交。
- 同步分支：`chore/sync-upstream-20261109`。保留双方历史，不重写 master 或已有功能分支。
- 核对时 fork 没有开放 PR。PR #13 的可选评分服务与 PR #14 的本地扫描暂停修复均已在当前 master 内，不从旧功能分支重做或覆盖。

上游新增提交：

1. `3b7197e`：环境变量及 Docker Compose 升级说明。
2. `d3c8654`：合并上述文档更新。
3. `a208f49`：JavDB 缓存路由返回 HTTP 403 后重新选路，最多重放原请求一次，并增加发现列表恢复回归。

## 合并结果与扩展边界

`internal/javdb/client.go` 和 `internal/javdb/client_test.go` 与上述上游 SHA 完全一致。生产代码只有一行变化：将 403 加入现有 502/503/504 的路由失败条件。保留 400/401、协议错误不重选，429 使用同路由退避，以及一次重放上限。

JavDB 独立评论使用同一个 `getJSON`，没有另建重试层。原始评分分制、评论分页、磁力日期聚合、默认下载选源、取消感知缓存、列表预加载、MDCz/Javinizer/字幕/TOP250、Emby 播放、多版本导出及本地扫描实现不变。全部前端、扩展挂载文件、依赖声明、锁文件和发布工作流与本轮 fork 基线相同。

唯一合并冲突在 README 升级章节。保留 Surc 的固定镜像、linux/amd64、完整停服备份、恢复与 Compose 服务限定操作；既有 `docker compose pull miyabi` / `up -d --no-deps miyabi` 已覆盖上游新增的通用升级步骤，不插入重复或弱化备份的版本。新增上游的 6 行反向代理回环提示、JWT/可信代理/日志环境变量和代理 IP 说明，不更新版本或发布状态。

## 本轮实际验证

环境：Linux 隔离工作区，Node `v24.22.1`，Python 标准库。没有安装工具或依赖。

| 检查 | 结果 |
| --- | --- |
| `node --test web/tests/*.node.mjs extensions/mdcz-bridge/*.test.mjs tools/javlibrary-browser/*.test.mjs` | 33/33 通过 |
| `node --test packaging/fnos/tests/*.test.mjs` | 18/18 通过 |
| `python -m unittest discover -s packaging/fnos/tests -p '*_test.py' -v` | 4/4 通过 |
| `git diff --cached --check`、无未解决冲突 | 通过 |
| 两个同步 JavDB 文件逐字节对比上游 | 一致 |
| 原有扩展目录、前端、依赖和锁文件对比 fork 基线 | 未变 |

**未完成：** 当前工作区及可连接的云电脑没有 Go SDK / module cache，`/usr/bin/go` 不是 Go 编译器；前端没有 `node_modules`。因此本轮没有执行 Go 回归（包括新 403 测试）、race、完整 Vitest、前端类型检查/lint/build 或生产构建。遵守仓库禁止自行安装依赖的约束，不将历史测试结果或静态比较当作本轮 Go 测试通过。PR 保持 draft，后续在工具齐备或获得安装授权后补齐验证。

Node 的浏览器服务和 fnOS 生命周期用例是本地夹具/模拟业务测试，不是实际浏览器或 NAS 验收。未访问真实 JavDB/JavLibrary 来源、未打标签、构建/发布镜像或 FPK，也未部署。

具备 Go 工具链与既有冻结依赖后，优先执行 JavDB 及其调用方回归，再补完整套件：

```sh
go test ./internal/javdb ./internal/netx ./internal/catalogue ./internal/api
go test -race ./internal/javdb ./internal/catalogue ./internal/api
# 按 docs/movie-detail-extensions.md 的干净 checkout 顺序构建前端，再执行完整检查。
```
