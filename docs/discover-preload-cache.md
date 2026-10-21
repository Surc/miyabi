# 作品列表预加载与持久缓存

基于 `Surc/miyabi:master@24db1ab1b98b6c4150daf7b9694ff0a31b0789d0`；上游为 `ppxb/miyabi@f1903459b45dece1498c05c7b309773776c756e9`。本扩展独立于已有详情页扩展与媒体库缓存，不修改旧 API、自动下载选源或来源排序。

## 用户行为

- 发现、分类、演员等元数据列表和搜索共用新列表组件。默认每批准备筛选后 6 页，每页 20 部；目标可配置为 1–24 页。
- 第一项筛选为含中文字幕。六页是至少 120 部去重、资料与磁力已就绪且符合筛选的作品，不是只获取六页原始结果。不够时继续扫描后续原始页。
- 只有成功返回的空页才视为来源到底。非空短页继续扫描；重复页显示分页异常；失败、暂停和部分来源结果保留重试入口，不冒充无结果。
- 资料/磁力请求并发为 2，已有结果可先浏览。当前批完成后隐藏整个补齐区域；进入下一批时再显示。沿用已确认的大区域布局，不采用后来取消的标题右侧小进度设计。
- 卡片按磁力显示独立的中文字幕与破解标签，保留来源原分制评分。中文字幕使用现有 `magnet.HasSubtitle`；破解只读取 `破解` 标签，不把所有无码资源当成破解。两个标签并列不保证同一磁力同时满足。
- 仅挂载当前展示页的 20 张卡片。未打开页只预取 JSON，不请求图片、预览或视频；图片首次打开时获取并持久保存。隐私模式继续隐藏图片。
- JavLibrary 评分只有当前页卡片挂载时才请求，独立 FIFO 队列最多并发 2；暂不可用显示 `—`，不阻塞资料/磁力补齐。

## 缓存策略

数据位于 `<MIYABI_DATA_DIR>/extensions/browse-v1/`，跨浏览器与服务重启复用；键使用摘要，不接受用户指定文件路径。

| 分类 | 存储与有效期 |
| --- | --- |
| `works` | 完整作品基础资料长期保存；评分、字幕与磁力数量不固化到基础资料 |
| `images` | 仅已打开作品的封面长期保存，图片地址从已读取的影片资料取得 |
| `ratings` | 按站点与影片隔离，默认 12 小时，可设 1–168 小时 |
| `resources` | 完整聚合磁力、中文/破解摘要及完整性，默认 30 分钟，可设 1–1440 分钟 |
| `indexes` | 原始分页/筛选结果索引，默认 5 分钟，可设 1–60 分钟 |
| `config` | 本机服务端的预加载设置，清理缓存时保留 |

首次读取过期资料返回最后一次成功快照，前端随后用 `refresh=1` 更新；失败保留旧数据和更新时间。部分磁力结果不作为完整快照持久化。设置为磁盘持久保存不等于无限保留内存：列表仅保留轻量摘要，完整的空闲详情与磁力在 QueryCache 中最多保留最近两批量级，已挂载详情不在该淘汰范围。

活跃页面按分钟检查磁力到期状态，过期后重新准备，暂停或失败状态不自动恢复。当前页评分定期检查更新。原始列表索引在再次读取/点击刷新列表时按有效期更新，当前浏览会话保留稳定顺序，避免定时重排导致正在看的卡片跳动。

“设置 → 作品列表缓存”或列表右侧按钮可调整设置、查看占用、清理图片/资料/全部扩展缓存。旧“清理未使用图片缓存”接口保持原样。清理仅影响扩展目录，不删除媒体库、任务、订阅或网盘内容；写入采用临时文件加原子替换，代次校验阻止清理前的旧请求恢复缓存。

## 接入点

新增后端 `internal/catalogue/browse_*.go` 与 `internal/api/browse_extension.go`，新增前端 `web/src/api/browse-cache.ts` 和 `web/src/features/discover/extensions/`。

已有文件仅负责装配/挂载：`internal/app/app.go`、`internal/api/router.go`、发现 `browse-results.tsx`、`metadata-search-page.tsx`、搜索 `page.tsx`、设置路由 `settings.tsx`。`web/src/api/client.ts` 只给受限的扩展图片路径增加原样通过规则，沿用已有图片组件和隐私设置。

新 API 均在原有鉴权、同源与 `no-store` 中间件内：

```text
GET    /api/extensions/browse/movies?...原分页/筛选参数...&refresh=0|1
GET    /api/extensions/browse/search?q=...&page=...&limit=20&refresh=0|1
GET    /api/extensions/browse/movies/:id/resources?refresh=0|1
GET    /api/extensions/browse/movies/:id/ratings/javdb|javlibrary?refresh=0|1
GET    /api/extensions/browse/movies/:id/image
GET    /api/extensions/browse/settings
PUT    /api/extensions/browse/settings
GET    /api/extensions/browse/cache
DELETE /api/extensions/browse/cache?scope=images|data|all
```

数据响应为 `{data,updated_at,stale,refresh_failed?}`；资源额外包含 `complete`，不能把未完成或过期的结果当成已确认无字幕。图片返回解码后的原始图片字节。扩展依赖为空时路由不挂载，旧接口及 golden 测试保留。

## 验证与升级

针对性用例覆盖筛选跨多页补齐、可配置批次、短页/重复页、暂停重试、部分/过期状态、StrictMode 生命周期、并发队列、当前页图片与评分渲染、完成后隐藏、持久化重启、分项清理、鉴权和输入验证。最终全套验证结果在完成本次开发后更新，不能把此前提交的通过结果当成本轮证据。

浏览器验收仍由用户完成；JavLibrary 真实来源当前未验收，不承诺更换代理或预取后评分站即可恢复。冷缓存整批准备受已有来源限速及网络影响，不承诺秒级完成。升级时保持上述挂载点，优先只调整独立适配器、存储版本与数据结构；不把动态资源状态或个人入库状态永久写成基础资料。
