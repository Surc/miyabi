# 详情页独立扩展与升级维护

作品列表的筛选后分页预加载、资源标签、评分与持久缓存见 [作品列表缓存维护说明](discover-preload-cache.md)。两套扩展独立挂载，保留原 API 与自动下载选源。

Emby 直接开播、作品名称番号前缀及后续 amd64 发布要求见 [Emby 播放扩展维护说明](emby-playback-extension.md)。

本扩展基于 `ppxb/miyabi` 的 `f1903459b45dece1498c05c7b309773776c756e9`（2026-10-15），开发分支为 `Surc/miyabi:feat/movie-detail-extras`。实现采用新增适配器、服务、接口和组件，原发现/媒体库 API、元数据评分、磁力聚合与自动下载选源逻辑保持原约定。此文档随实现提交，供后续同步上游时使用。

## 功能与边界

- 磁力卡片展示现有 `created_at`。排序提供默认推荐、发布时间最新优先、最早优先；只排序前端副本。未知或非法日期在两种日期排序中均置底，相同时间保持来源顺序。默认推荐保持上游返回顺序。
- 同一磁力哈希的日期仍遵循上游聚合约定：取多个来源中最早的非空值。本扩展不把影片发行日期、文件修改时间或本地发现时间当作磁力发布时间。
- JavLibrary 评分独立显示原始 10 分制；已有 JavDB/FANZA 评分及其分制保持原样，不写入数据库。区分加载、未收录、已收录但未评分、来源异常和重试。
- JavDB 与 JavLibrary 评论使用独立页签和缓存。只请求当前页签，逐页加载，按稳定评论 ID 去重；后一页失败时保留已有内容并允许重试。上游错误或挑战页面不伪装成“暂无评论”。
- 媒体库详情始终显示已保存元数据；JavLibrary 仅依赖番号，无需等 JavDB 可用。未关联 JavDB 的影片可直接显示 JavLibrary 评论，JavDB 页签说明缺少关联。
- 复用现有 `preview_video`。有效、无账号凭据的绝对 HTTPS 地址才显示第一格预告入口；无地址时保留原预览图顺序。图片查看器仍接收原数组和原索引。
- 预告只在点击后挂载原生视频，关闭、切换影片或启用隐私模式时暂停并清除媒体地址。隐私模式关闭后也不会自动重新打开旧预告。
- HLS/M3U8 依赖浏览器原生支持，未增加解码依赖或转码服务。不支持或加载失败时提供说明、重试与源链接。视频使用浏览器自身网络，不经过后端代理或图片接口；后端能访问图片不代表该浏览器能播放视频。

## 新接口

路由位于现有鉴权和同源检查内，响应设为 `no-store`：

```text
GET /api/extensions/movie/javlibrary-rating?code=ABP-123
GET /api/extensions/movie/comments?source=javdb&javdb_id=<id>&page=1
GET /api/extensions/movie/comments?source=javlibrary&code=ABP-123&page=1
```

番号和标识最多 200 字符，页码为 1–1000，只接受上述两种来源。请求不接受任意目标 URL、Cookie、令牌或第三方账号。参数错误返回 400；来源失败返回 502；取消使用现有取消语义，不缓存失败。

评分示例：

```json
{"source":"javlibrary","found":true,"score":8.5,"max":10}
```

`found=false, score=null` 表示未匹配；`found=true, score=null` 表示匹配影片暂无评分。

评论示例：

```json
{"source":"javdb","page":1,"has_more":false,"items":[{"id":"123","author":"reader","content":"评论正文","created_at":"2026-10-21","rating":4.5,"rating_max":5,"likes":3}]}
```

`items` 始终为数组。`rating` 和 `likes` 可缺省，JavLibrary 的“有用”投票不冒充用户影片评分。正文输出为纯文本，前端依靠 React 转义，不使用 `dangerouslySetInnerHTML`。

## 模块与最小接入点

| 新增模块 | 职责 |
| --- | --- |
| `internal/domain/movie_extras.go` | 独立评分、评论与分页模型 |
| `internal/javdb/reviews.go` | 扩展匿名评论读取，复用原签名、限流、路由和代理 |
| `internal/javlibrary/` | 番号精确匹配、评分/评论解析、受限网络请求 |
| `internal/catalogue/movie_extras.go` | 可选窄接口、来源隔离与短期缓存 |
| `internal/api/movie_extras.go` | 独立扩展 API 与参数验证 |
| `web/src/api/movie-extras.ts` | 独立 React Query keys、取消与分页 |
| `web/src/features/movie-detail/extensions/` | 日期排序、独立评分、评论与预告组件 |

仅以下六个上游生产文件增加挂载点：

| 文件 | 改动 |
| --- | --- |
| `internal/app/app.go` | 创建扩展服务、注入、关闭 |
| `internal/api/router.go` | 可选依赖与受保护路由挂载 |
| `web/src/features/movie-detail/hero.tsx` | 可选 `extraStats` 插槽 |
| `web/src/features/movie-detail/content.tsx` | 挂载扩展与传递已有字段 |
| `web/src/features/movie-detail/magnets.tsx` | 本地排序状态与日期显示 |
| `web/src/features/movie-detail/previews.tsx` | 可选第一格插槽，保持图片索引 |

`CatalogueManager` 与 `JavDBClient` 原接口不增加方法；评论通过新的可选窄接口判断能力，旧 fake 和替代客户端不必实现它。未配置扩展依赖时新路由不挂载。

## 来源适配与缓存

JavDB 使用 `/api/v1/movies/{id}/reviews?page=N&limit=20`。分页按原始记录数判断，纯打分而无正文的记录不会提前结束后续页。缺少 `reviews` 字段视为响应结构变化；明确的 `null` 或空数组才视为空结果。用户评分保留 5 分制。

JavLibrary 固定访问 `https://www.javlibrary.com/cn/`，使用已有代理管理器，每次请求 15 秒超时、最多 2 MiB，最多跟随两次同源重定向（第三次被 `len(via) >= 3` 拒绝）。先通过 `vl_searchbyid.php` 精确匹配完整标准化番号，再读取影片；多个不同匹配 ID、身份不一致、无法识别的页面均报错。独立错误分类、脱敏请求诊断、页面证据边界及飞牛验证步骤见 [JavLibrary 请求诊断](javlibrary-diagnostics.md)。

主要选择器为 `.videos a .id`、`#video_id .text`、`#video_review .score`、`#video_comments table.comment` 下的 `.info`、`.t`、`.date`。只接受同影片的下一页链接。缺少站点评论 ID 时以作者、日期和正文的摘要生成稳定 ID，避免重复展示。站点页面变化优先只调整此适配器和其夹具。

使用上游现有取消感知的 `responseCache`，最后一个等待者离开时取消请求：影片查找 256 项/10 分钟，由评分与评论共享；评论按来源、影片和页码隔离，512 项/2 分钟。失败不缓存，JavDB 仍保留原活动路由持久化。

## 测试与复核

本任务用户已授权补齐测试工具和依赖；保留现有 `go.mod`、`go.sum`、`web/package.json` 和冻结锁文件，不新增产品依赖。工具版本：Go 1.27.3、Node 24.19.0、pnpm 10.33.0。

干净 checkout 先构建前端，生成路由类型与 `web/dist`，再进行类型检查和默认 Go 构建：

```sh
cd web
corepack pnpm@10.33.0 install --frozen-lockfile
corepack pnpm@10.33.0 build
corepack pnpm@10.33.0 test
node --test tests/*.node.mjs
corepack pnpm@10.33.0 typecheck
corepack pnpm@10.33.0 lint
corepack pnpm@10.33.0 fmt:check
cd ..
go test ./...
go build ./cmd
git diff --check
```

Go SDK 可通过命令完整路径或临时 PATH 使用，不把运行器临时目录写进项目配置。`.node.mjs` 用 Node 原生 TypeScript 类型剥离运行，不纳入 Vitest 的 include。全量 Go 番号语料测试可能较慢，不能当作挂起而跳过。

2026-10-21 本次重建版本已重新验证：完整 Go 套件 36 个测试包通过（保留原有 19 个测试/子测试 skip 事件），Vitest 32 文件/210 测试通过，Node 独立逻辑 14 测试通过；TypeScript、Oxlint、Oxfmt、修改 Go 文件的 gofmt、差异空白检查、前端生产构建和 Go 可执行文件构建通过。旧提交的测试结果不作为本版本证据。

全量 Go 首次运行暴露上游已有的 `TestProxiedFingerprintClientSwitchesProxyAndCloses` 夹具不匹配：tls-client 通过 CONNECT 访问代理，而旧测试服务器只返回普通 HTTP 响应。独立新增 `internal/netx/fingerprint_proxy_test.go` 模拟 CONNECT 隧道，原测试仅替换两个代理服务器的构造，保留全部切换和关闭断言；生产网络代码未改。上游修复相同夹具后可移除这个测试专用补丁。

本轮 JavLibrary 实际 HTTP 探测返回 195 字节的 `Site Unavailable` 页面，无法在此环境验证真实评分或评论页面；不代表站点全局不可用。JavDB 在线评论与浏览器实际播放仍未验收，应在对应网络与浏览器上确认。为此 PR 保持 draft，不自动合并或发布。

浏览器验收仍由用户完成：查看默认/双向日期排序及未知日期、两站评分并列、切换评论与加载更多/失败重试、无 JavDB 关联的媒体库详情、MP4/HLS 播放、关闭/切换影片/隐私模式释放媒体及无预告回退。离线夹具测试不能证明第三方在线可用性或实际浏览器解码能力。

## 同步上游的操作顺序

1. 记录 fork/upstream 实际最新 SHA，保留现有分支，优先合并上游再调整上述六处挂载点。
2. 核对上游是否已有等价功能，避免重复挂载；不得覆盖已有评分或重新排序自动下载候选。
3. 核对 `created_at`、同哈希聚合、缓存取消、网络代理与鉴权约定；变化集中在扩展适配层解决。
4. 若评论结构改变，保存脱敏真实响应/页面证据，更新适配器与行为测试。挑战页面和请求失败不得改为空成功来使测试通过。
5. 完整运行上述检查，更新本文与 `maintain-miyabi` skill 的基线、远端分支、提交和验证限制。
6. 每个可验证阶段及时同步远端。推送后读回并核对 Git tree/文件，不能把本地 commit 当成已远端保存。发布流程仅监听版本标签；没有发布授权不打标签、发布镜像或部署。
