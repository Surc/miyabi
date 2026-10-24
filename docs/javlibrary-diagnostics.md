# JavLibrary 请求诊断与飞牛验证

## 范围与基线

当前诊断实现由 [PR #10](https://github.com/Surc/miyabi/pull/10) 合入，提交为 `9570c514a36458c71ca00a036d3214778d22a405`；[v0.4.2 Release](https://github.com/Surc/miyabi/releases/tag/v0.4.2) 已于北京时间 2026-10-24 05:38:55 公开，[发布运行 40028000713](https://github.com/Surc/miyabi/actions/runs/40028000713) 成功。代码回归基线为 v0.4.1 的 `e00d4e233164b7e7c4f988d242ee93cb0f4278de`，结果见 [v0.4.2 回归记录](validation-javlibrary-v0.4.2.md)。发布成功不代表真实站点或 NAS 已验收。

2026-10-24 已逐项对照 [PR #2](https://github.com/Surc/miyabi/pull/2)（`bb4e2453dba7d5d64873a15fe98a453cd45a4be9`）的历史清单。该草稿只新增 `docs/javlibrary-follow-up.md`，没有独立业务修复需要合并；有效验收要求已归入本文。PR #2 按被 PR #10 和本文替代处理，不将旧清单合入 master，也不再维护第二份问题清单。历史运行器错误不作为 NAS 根因。

保留 `New(proxy)`、原始 10 分制、抓取路径、选择器、HTTP/API 响应结构及缓存行为。JavDB/FANZA、自动下载、全局网络配置和发布流程均不变。

## PR #2 逐项对照

| 原记录 | PR #10 / v0.4.2 已确认的变化 | 当前结论 |
| --- | --- | --- |
| JL-01：运行器 DNS/网络阻塞 | 已能分类 DNS、网络、TLS、超时等失败，保留原始错误链并输出允许的脱敏 cause 字段 | 诊断能力已实现；2026-10-21 的 `network is unreachable` 只证明当时运行器直连受限，NAS 根因仍待 A1、A2 |
| JL-02：应用内代理约定 | 在 Transport 实际选择代理时记录协议和主机/端口，区分代理连接、CONNECT 状态和站点 HTTP 状态；本地用例覆盖动态代理切换及 CONNECT 407/403 | 代理观察能力已实现，公共网络约定未改；不能用 Docker 的 `HTTP_PROXY/HTTPS_PROXY` 或宿主机 curl 成功替代 A2 |
| JL-03：195 字节 `Site Unavailable`，无正常页面 | 区分非 200、明确异常/挑战页、未知 HTML、身份不符、评分格式和正文超限，并提供无正文 DOM 摘要 | 异常页诊断已实现；正常评分、搜索/空结果和评论分页仍待 A3、A4。历史响应不能证明源站故障或 Cloudflare 拦截 |
| JL-04：错误可诊断性待改善 | 记录 search/detail/comments、阶段、实际代理、HTTP/CONNECT 状态、重定向拒绝原因、长度和耗时；日志脱敏、原始 cause 可 Unwrap，对外仍为通用 502 | 本地实现与四包普通/race 回归已完成，不再列为待实现；真实网络故障的归因仍依赖 A1、A2 的部署证据 |

旧清单的后续顺序和完成条件按下列方式承接；待验收项只在下文维护：

| PR #2 要求 | 承接位置与状态 |
| --- | --- |
| P1：收集部署端证据、确认实际网络路径 | A1、A2，均未完成 |
| P2：核对评分/搜索页面、单独验收评论 | A3、A4，均未完成 |
| P2：评估请求层补强或更清晰的错误分类 | 错误分类已由 PR #10 完成；会话、指纹、站点/语言或跳转兼容仅在真实证据支持时按 A5 评估，不预设需要修改 |
| P3：针对真实复现补充回归 | 诊断的本地回归已完成；来源特有的脱敏夹具和回归由 A5 承接，尚未完成 |
| 完成条件：NAS 原始 10 分制评分及独立评论/分页 | A2–A4，均未完成 |
| 完成条件：失败不伪装成空结果、最小改动、测试与线上/浏览器证据分开 | 已识别失败及空结果的本地回归通过；真实页面内部字段变化仍待 A3–A5。原接口、JavDB/FANZA、选源和磁力顺序继续保留 |

### 历史源码对照的用途

PR #2 的 2026-10-21 源码对照保留为候选方案的证据索引，并非本轮重新核对的上游状态：[MDCx 固定快照](https://github.com/sqzw-x/mdcx/blob/58e3f930f2e864fceb8a53ceef818716e2a6413d/mdcx/crawlers/javlibrary.py) 使用相同评分选择器，没有评论实现；其语言路径、会话、指纹和代理差异不能证明 Miyabi 当前选择器错误或评论可用，也不照搬关闭 TLS 校验的设置。[MDCz 固定快照](https://github.com/ShotHeadman/mdcz/tree/f65e7d4161151aff1531f3b99872e49234d5d539) 当次检索未发现已注册的 JavLibrary 来源，不能当作现成爬虫直接移植。后续只有 A3、A4 的真实响应证明有必要时才评估这些方向。

## 最小接入

- `internal/javlibrary/diagnostics.go`：请求本地的诊断状态、错误分类及 Transport 观察回调。
- `internal/javlibrary/page_diagnostics.go`：明确异常页识别和不含文本的 DOM 特征摘要。
- `internal/javlibrary/client.go`：接入实际代理选择、CONNECT、重定向、HTTP、正文及解析阶段信息。
- `internal/javlibrary/parser.go`：原错误改为带类别/原因的错误，匹配和空结果规则保持。
- 无 API、前端、数据库或公共 `netx` 生产代码接入点。`sourceError.Unwrap()` 保留原始错误链；错误文本只输出允许的诊断字段，外层 `extrasError()` 继续提供通用 502。

移除时恢复 client/parser，并删除两个诊断文件即可；没有需要迁移的数据或新增依赖。

## 日志含义

默认日志等级下，失败仍只产生原有请求错误日志。其 `error` 字段增加：

| 字段 | 含义 |
| --- | --- |
| `attempt` | 当前进程内单次出站操作的编号；重定向共用，重启后重新计数 |
| `operation` | `search`、`detail` 或 `comments`；评论之前的查找失败仍明确标为 search/detail |
| `phase` | request、redirect、response、body 或 parse；未增加全链路 httptrace，不能据此细分所有 TCP/TLS 超时 |
| `category` / `reason` | 稳定类别和具体原因，见下表 |
| `target` / `redirect_target` | 请求目标和拒绝的跳转目标；去掉用户信息、查询和片段，限制长度 |
| `proxy` | Transport 真正选择的代理协议及主机/端口；`direct` 为应用直连，`not_resolved` 为未观察到代理选择。无法据此证明宿主机没有透明代理/TUN |
| `http_status` / `proxy_status` | 站点 HTTP 状态与 HTTP 代理 CONNECT 状态分开；0 表示未观察到，可能是连接复用或相应阶段未执行 |
| `redirects` | 尝试跟随的跳转序号，包含被策略拒绝的那一跳 |
| `content_type` / `bytes` | MIME 类型不含参数；实际读取字节数，上限错误最多读取 2 MiB + 1 字节 |
| `elapsed_ms` / `shape` | 单次 HTTP 操作耗时及 DOM 节点存在性/数量，不含影片名、评分内容或评论正文 |

类别包括 `dns`、`proxy_connect`、`proxy_http`、`tls`、`timeout`、`network`、`redirect`、`http_status`、`body_limit`、`unexpected_page`、`identity`、`parse_score` 和 `canceled`。DNS/代理连接错误保留原始 cause，可能同时包含 timeout；类别不是完整网络追踪。取消仍沿用应用的取消语义。额外诊断仅输出底层类型、DNS 主机及超时/未找到标志、网络操作、系统 errno 及其标准描述，不拼接任意底层错误正文。

只有 HTTP 200 的页面才进行 HTML 识别。非 200 直接报告 HTTP 类别，不读取错误正文，不把 403 直接称作 Cloudflare。`site_unavailable` 只说明收到该类页面，不能确定由源站还是中间层生成。挑战页需匹配受限标题和明确 DOM 标记；不尝试绕过挑战。

`MIYABI_LOG_LEVEL=debug` 时额外记录 `JavLibrary response`：表示 HTML 已接收并检查，**不表示影片身份/评分/评论已验收成功**。只记录路由、长度和 DOM 摘要，不包含原始 HTML、标题、Cookie、查询参数或错误 cause。诊断编号可与后续失败日志关联；多个 API 等待者共享查找时可能对应同一个 attempt。

原访问日志的 `id` 用于定位 API 请求。未提供入站 ID 时，中间件自动生成并写入响应头 `X-Request-Id`；已有入站 ID 时日志沿用它，不保证再次回显到响应头，可结合请求头和时间定位。Go 的重定向错误可能包含相对 Location 或畸形响应头，原访问日志的绝对 URL 脱敏不能覆盖所有情况，因此此模块仅打印允许的 cause 诊断字段，原始错误对象仍通过 Unwrap 保留。主机/IP/路径仍可能存在，分享前替换私人地址。不要分享完整代理配置或登录 Cookie。

## 飞牛最短验证

1. 记录部署版本。打开 Miyabi 网络代理设置，确认已启用、已保存，刷新后仍保持。`GET /api/settings/network` 可核对已保存配置，但其中地址可能含凭据，只记录脱敏结果。设置页“测试连接”只测试 JavDB/JavBus，也可使用未保存输入，不能证明 JavLibrary 已走代理。
2. 使用已登录的同源浏览器请求 `/api/extensions/movie/javlibrary-rating?code=<实际番号>`，再请求 `/api/extensions/movie/comments?source=javlibrary&code=<实际番号>&page=1`；返回 has_more 时检查第 2 页。记录时间、状态、`X-Request-Id`。选近期未查询的已知影片，或等待已有影片缓存自然过期，不清缓存、不修改代理来制造故障。
3. Docker 安装读取 `docker logs --since 10m --timestamps '<实际容器名>'`；原生安装读取 Miyabi 进程对应日志。通过请求 ID 找到单条失败，检查 operation/category/reason 与代理/HTTP 状态。入口反代返回 HTML 502 且无对应应用日志时，先查入口和进程。
4. 用同一时间窗口的代理连接/流量记录交叉验证 Miyabi 到 `www.javlibrary.com:443` 的访问；复用连接未必产生新 CONNECT。应用日志 proxy=direct 仅指未使用应用内显式代理，不排除宿主机透明代理。

影片查找仍缓存 10 分钟、评论 2 分钟，失败不缓存。直接扩展接口避开列表额外持久缓存，但仍会使用影片/评论缓存。`Cache-Control: no-store` 和浏览器刷新不绕过这些服务端缓存。默认日志没有出站成功摘要；如需观察正常空结果，后续经用户安排启用 debug，或取得同网络路径的脱敏 HTML，不为诊断新增清缓存接口。

## 真实来源与 NAS 验收

截至 2026-10-24，以下各项均为**未完成**；本节是唯一的 JavLibrary 待验收清单。本次只归并文档，没有部署 NAS、调用真实来源或重新执行历史业务回归。后续在本节更新状态、日期和脱敏证据，不重新打开 PR #2 维护另一份清单。

| 编号 | 状态 | 验收要求和完成证据 |
| --- | --- | --- |
| A1 | 未完成 | 记录实际 Miyabi 版本/提交、影片番号、北京时间（UTC+8）的失败或验证时间、接口状态、请求 ID、已保存应用内代理的启用状态，以及 operation/category/reason 和脱敏地址。只收集必要日志，不收集 Cookie、令牌或代理凭据 |
| A2 | 未完成 | 在用户 NAS 上通过 Miyabi 实际应用内代理路径，结合应用日志与同一时间窗口代理连接/流量记录核对 DNS、代理连接、TLS、HTTP 和正常页面。明确未观察到的阶段，不把 proxy=direct 当作没有透明代理，也不把宿主机 curl 或设置页测试连接当作扩展验收 |
| A3 | 未完成 | 保存同网络路径的脱敏真实页面，覆盖搜索列表、直接跳转详情、未收录、已收录未评分和多结果；确认至少一部已收录影片的完整番号匹配及原始 10 分制评分。核对 `.videos` 内部字段、`#video_review .score` 缺失与合法空结果的区别，不能仅凭 HTTP 200 或 debug 摘要判定成功 |
| A4 | 未完成 | 独立验收真实评论字段、空评论、多页和最后一页；核对同影片身份、has_more/最终页码及重定向页码回退、稳定 ID 去重、下一页失败后保留已有内容并能重试。评分成功或 MDCx 的评分实现不能替代评论验收 |
| A5 | 未完成，依真实证据推进 | 对 A2–A4 实际复现的问题补充脱敏夹具或本地网络回归，覆盖失败不缓存、取消、代理变更、影片身份及失败不伪装为空成功。只有证据支持时才另行修复会话/指纹/站点/语言/跳转或页面结构；优先限制在 `internal/javlibrary/`，保留原构造函数、旧 API、分制、JavDB/FANZA、下载选源和默认磁力顺序。分别记录本地回归、线上来源和用户浏览器结果，再更新本文及维护记录 |

PR #2 还提到 JavDB 在线评论与浏览器实际预告播放。这两项仍未验收，继续由 [详情页扩展维护文档的浏览器验收](movie-detail-extensions.md#测试与复核) 承接，不作为 JavLibrary 修复已完成的证据，也不在此复制清单。

## 保留的证据边界

- 分制：参考 [JAVMovieScraper 的 JavLibrary 适配器](https://github.com/DoctorD1501/JAVMovieScraper/blob/2099948d716a61166bd4a4ae0bfffc43b4e77f18/src/main/java/moviescraper/doctord/controller/siteparsingprofile/specific/JavLibraryParsingProfile.java#L161) 明确按 `Rating(10, ratingText)` 读取原始 `.score`。本轮无法取得真实站点页面，不能宣称重新完成当前线上分制或选择器验收；不进行五分换算。
- PR #10 / v0.4.2 没有解决 `.videos` 内部字段变化、评论行正文节点缺失与合法空结果的区分，也没有校验评论重定向的最终页码。后续响应校验修复见下节，不将这些新增行为倒记为 v0.4.2 已包含。
- 即使补充响应校验，已有 `#video_review` 中缺少 `.score`、非预期评论行类型或语义变化仍可能与合法空结果无法区分；保留已有无评分、空评论和完整番号匹配约定。URL 页码正确也不能证明正文实际来自该页，仍需 A3、A4 的真实页面。
- 重定向策略未放宽：`len(via) >= 3` 在第三跳拒绝，即最多跟随两跳。原详情文档的“三次”已按实现纠正。
- 运行器旧 DNS 错误和 195 字节 Site Unavailable 页面不能证明 NAS、源站或 Cloudflare 的状态。没有在线评分、评论分页、NAS 或浏览器验收结果。

## 后续响应校验修复（2026-10-24，未发布）

基于 master 的文档归并提交 `a5ed66c`，用户要求“修复”后，仅在 `internal/javlibrary/client.go`、`parser.go` 内调整响应校验；没有修改公共 netx、缓存生产代码、旧 API 或前端。独立评分仍为原始 10 分制，代理、来源路径、跨源限制、超时、正文上限与脱敏诊断继续沿用。

- 评论最终 URL 的 `v` 必须唯一且与目标影片一致，页码必须唯一、有效并等于请求页。允许第 1 页规范化时省略 page，后续页不能省略或回退；失败使用 `identity/comments_redirect_changed_page`，不会以请求页码缓存其他页的内容。
- 下一页识别支持当前评论路径的纯查询链接及等值数字页码，拒绝重复 v/page 和仅以后缀伪装的其他 PHP 端点；仍不跟随外站链接，也不改变稳定评论 ID 去重。
- 搜索中存在可识别影片目标却缺少番号节点，或目标番号的链接缺少唯一有效影片 ID 时，返回 `unexpected_page/search_identity_missing` 或 `search_link_invalid`，不当作未收录。同一影片的海报链接可由另一条含番号的链接补足身份；真正空列表及正常的其他番号结果仍有效。
- 已识别的 `table.comment` 缺少 `.t` 正文节点时，返回 `unexpected_page/comment_body_missing`，不静默跳过而报告空评论或部分成功。空评论容器、存在但内容为空的正文仍保留原行为。
- 有值评分只接受普通数字或完整成对括号包裹的数字；单侧括号不再被当作有效评分。合法原始分数、范围和无评分状态保持。

以上是本地可复现缺陷的修复，不是实际来源验收。当天运行器的一次 urllib 探测仍得到 HTTP 200、195 字节 `Site Unavailable`，没有正常列表/详情/评论标记；这是运行器请求路径的结果，未使用 NAS 的实际应用内代理，不能替代 A1–A5 或确定故障根因。代码回归结果见 [回归记录](validation-javlibrary-v0.4.2.md#后续响应校验修复2026-10-24未发布)，唯一待验收清单仍为上文 A1–A5。

## 验证状态

2026-10-24 已在合入当前 master 的隔离工作区运行并通过 JavLibrary、netx、catalogue、API 四包 Go 回归及同四包 race 检测，各记录 414 个测试/子测试通过事件、无失败或跳过。gofmt 与 `git diff --check` 通过。生产构建、原生 smoke、FPK 和镜像由合并后的发布工作流执行，最终产物状态以 Release 为准。

已运行的回归用例包括：错误类型与取消链保留；相对 Location/畸形响应头错误不泄漏原文；实际代理动态切换及 CONNECT 407/403；HTTP/异常页/未知 HTML/超限区分；两跳通过、第三跳拒绝；真正返回错误影片的身份保护；合法空结果和评论词语不误判；debug 摘要不含敏感文本；真实适配器失败经 API 返回通用 502、访问日志脱敏且保留请求 ID。API 夹具分别覆盖生成 ID 与沿用入站 ID，不改变日志中间件行为。

后续仍需在 NAS 的实际应用内代理路径取得正常页面，分别验收评分、空结果、评论和分页；不能将本地 fixtures 或发布启动检查视为真实来源已恢复。
