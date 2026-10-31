# 作品列表预加载与持久缓存

基于 `Surc/miyabi:master@24db1ab1b98b6c4150daf7b9694ff0a31b0789d0`；上游为 `ppxb/miyabi@f1903459b45dece1498c05c7b309773776c756e9`。本扩展独立于已有详情页扩展与媒体库缓存，不修改旧 API、自动下载选源或来源排序。

## 用户行为

- 发现、分类、演员等元数据列表和搜索共用新列表组件。默认每批准备筛选后 6 页，每页 20 部；目标可配置为 1–24 页。
- 第一项筛选为含中文字幕。六页是至少 120 部去重、资料与磁力已就绪且符合筛选的作品，不是只获取六页原始结果。不够时继续扫描后续原始页。
- 只有成功返回的空页才视为来源到底。非空短页继续扫描；重复页显示分页异常；失败、暂停和部分来源结果保留重试入口，不冒充无结果。
- 资料/磁力请求并发为 2，已有结果可先浏览。2026-10-23 用户更新要求：移除独立补齐区域，加载状态显示在当前页卡片上；资料未到时显示骨架，完成后隐藏卡片加载提示。工具栏保留小型暂停/继续/重试按钮，不显示整体进度面板。
- 卡片按磁力显示独立的中文字幕与破解标签，保留来源原分制评分。中文字幕使用现有 `magnet.HasSubtitle`；破解只读取 `破解` 标签，不把所有无码资源当成破解。两个标签并列不保证同一磁力同时满足。
- 已入库卡片另在封面左下角显示实际文件版本，不以网络磁力属性替代；判定、独立 API 和升级边界见 [入库版本卡片](library-card-extensions.md)。
- 仅挂载当前展示页的 20 张卡片。未打开页只预取 JSON，不请求图片、预览或视频；图片首次打开时获取并持久保存。隐私模式继续隐藏图片。
- JavLibrary 评分只有当前页卡片挂载时才请求，独立 FIFO 队列最多并发 2；暂不可用显示 `—`，不阻塞资料/磁力补齐。

## 缓存策略

数据位于 `<MIYABI_DATA_DIR>/extensions/browse-v1/`，跨浏览器与服务重启复用；键使用摘要，不接受用户指定文件路径。

| 分类 | 存储与有效期 |
| --- | --- |
| `works` | 完整作品基础资料长期保存；评分、字幕与磁力数量不固化到基础资料 |
| `references` | 从已读取列表/详情保存番号与封面地址；图片和可选评分无需先额外请求完整详情 |
| `images` | 已打开作品的封面及图片代理实际读取的预览、演员图片长期保存，不预取未浏览图片 |
| `ratings` | 按站点与影片隔离，默认 12 小时，可设 1–168 小时 |
| `taxonomy` | 分区标签分类持久保存，默认 24 小时 |
| `resources` | 完整聚合磁力、中文/破解摘要及完整性，默认 30 分钟，可设 1–1440 分钟 |
| `indexes` | 原始分页/筛选结果索引，默认 5 分钟，可设 1–60 分钟 |
| `config` | 本机服务端的预加载设置，清理缓存时保留 |

首次读取过期资料返回最后一次成功快照，前端随后用 `refresh=1` 更新；失败保留旧数据和更新时间。部分磁力结果不作为完整快照持久化。设置为磁盘持久保存不等于无限保留内存：列表仅保留轻量摘要，扩展写入的空闲详情与磁力在 QueryCache 中最多保留最近两批量级，按实际使用顺序淘汰；已挂载详情不在该淘汰范围。预加载结果同时填入原详情查询缓存，打开详情可以复用，旧接口及其刷新策略保持原样。

活跃页面按分钟检查磁力到期状态，过期后重新准备，暂停或失败状态不自动恢复。当前页评分定期检查更新。原始列表索引在再次读取/点击刷新列表时按有效期更新，当前浏览会话保留稳定顺序，避免定时重排导致正在看的卡片跳动。

列表与详情的 JavLibrary 评分共用按规范化番号保存的持久缓存。评分先通过 `cached_only=1` 读取服务端缓存，缓存读取不占用上游请求队列；未命中或过期时再进入最多两个并发的更新队列。过期分数立即显示，刷新失败保留分数及原始更新时间并允许重试。旧的按影片 ID 保存的 JavLibrary 分数仍可在列表缓存读取中显示，新请求写入共用番号缓存。

“设置 → 作品列表缓存”或列表右侧按钮可调整设置、查看占用、清理图片/资料/全部扩展缓存。设置为 NAS 服务端共享配置。旧“清理未使用图片缓存”接口保持原样。清理仅影响扩展目录，不删除媒体库、任务、订阅或网盘内容；写入采用临时文件加原子替换，代次沿嵌套请求传递，阻止清理前的旧请求恢复缓存。

同一窗口提供“导出缓存”和“导入缓存”。ZIP 包含上述资料、演员与标签字段、评分、资源摘要、列表索引、标签分类和已缓存图片，保留原始更新时间；不包含服务端配置、登录信息或媒体库。导入最大 512 MB，解压总量最多 2 GB，单条最多 64 MB。只接受带版本标识的本应用缓存包，全部记录校验通过后才合并，已有且时间不早于导入记录的本地数据会保留。导入推进缓存代次，阻止之前的请求写回；成功后刷新当前浏览会话和评分查询。

## 接入点

新增后端 `internal/catalogue/browse_*.go` 与 `internal/api/browse_extension.go`，新增前端 `web/src/api/browse-cache.ts` 和 `web/src/features/discover/extensions/`。

已有文件仅负责装配/挂载：`internal/app/app.go`、`internal/api/router.go`、发现 `browse-results.tsx`、`metadata-search-page.tsx`、搜索 `page.tsx`、设置路由 `settings.tsx`。`web/src/api/client.ts` 只给受限的扩展图片路径增加原样通过规则，沿用已有图片组件和隐私设置。

新 API 均在原有鉴权、同源与 `no-store` 中间件内：

```text
GET    /api/extensions/browse/movies?...原分页/筛选参数...&refresh=0|1
GET    /api/extensions/browse/search?q=...&page=...&limit=20&refresh=0|1
GET    /api/extensions/browse/movies/:id/resources?refresh=0|1
GET    /api/extensions/browse/movies/:id/ratings/javdb|javlibrary?refresh=0|1
GET    /api/extensions/browse/movies/:id/ratings/javdb|javlibrary?cached_only=1
GET    /api/extensions/browse/ratings/javlibrary?code=...&refresh=0|1&cached_only=0|1
GET    /api/extensions/browse/tags?zone=...&refresh=0|1
GET    /api/extensions/browse/movies/:id/image
GET    /api/extensions/browse/settings
PUT    /api/extensions/browse/settings
GET    /api/extensions/browse/cache
DELETE /api/extensions/browse/cache?scope=images|data|all
GET    /api/extensions/browse/cache/export
POST   /api/extensions/browse/cache/import
```

数据响应为 `{data,updated_at,stale,refresh_failed?}`；资源额外包含 `complete`，不能把未完成或过期的结果当成已确认无字幕。图片返回解码后的原始图片字节。扩展依赖为空时路由不挂载，旧接口及 golden 测试保留。

评分的 `cached_only=1` 未命中时返回 JSON `null`，不联系评分来源。导出返回 `application/zip` 附件，导入直接上传 ZIP 请求体并返回合并后的缓存统计；二者沿用鉴权、同源保护和 `no-store`。

2026-10-31 缓存迁移验证：新增前端用例覆盖刷新队列占满时先显示旧分数、刷新失败保留分数与重试、二进制 ZIP 传输和接口错误信息。TypeScript、Oxlint、修改文件 Oxfmt 与前端生产构建通过。使用已安装的 Go 1.24，在独立临时模块复制实际缓存实现及其领域类型，隔离不参与缓存测试的主服务依赖后，17 个缓存用例通过；覆盖重启恢复演员/标签/图片/评分、时间戳合并、无效包不落盘以及导入前请求无法写回。项目要求 Go 1.27，本机未安装该版本，完整 Go 套件与新增 API 集成用例尚未运行；未下载工具链或安装依赖。浏览器验收由用户完成。

## 验证与升级

初版发布时通过：完整 Go 套件 36 个测试包、`go test -race ./internal/catalogue ./internal/api`、Vitest 35 文件/222 测试、Node 14 测试、TypeScript、Oxlint、Oxfmt、修改文件 gofmt、diff 空白检查、前端生产构建和原生 Go 可执行文件构建。未修改 README、依赖声明或锁文件。2026-10-23 卡片版本/加载调整后的结果见 [最新验证记录](validation-2026-10-23.md)；该轮恢复测试，但未恢复正式产物构建。

回归用例覆盖筛选跨多页补齐、可配置批次、短页/重复页、暂停重试、部分/过期状态、StrictMode 生命周期、并发队列、当前页图片与评分渲染、完成后隐藏、持久化重启、分项清理、鉴权和输入验证。另验证清理时嵌套请求不会写回、封面/评分复用列表信息而不触发详情请求、相同来源更新时间下按最近使用淘汰缓存、预加载磁力与原详情查询复用。渲染测试为 SSR/夹具测试，不代表浏览器或线上来源已经验收。

浏览器验收仍由用户完成；JavLibrary 真实来源当前未验收，不承诺更换代理或预取后评分站即可恢复。冷缓存整批准备受已有来源限速及网络影响，不承诺秒级完成。升级时保持上述挂载点，优先只调整独立适配器、存储版本与数据结构；不把动态资源状态或个人入库状态永久写成基础资料。
