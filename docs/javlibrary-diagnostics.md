# JavLibrary 请求诊断与飞牛验证

## 范围与基线

2026-10-23 在 `Surc/miyabi:master@1e142f413de081754613bee567b5024eca2ac084` 上实施独立请求诊断。用户在源码排查后确认“按你的来”，本阶段只提交诊断实现及回归用例，仍不编译、不部署、不发布。

前次问题清单为 [PR #2](https://github.com/Surc/miyabi/pull/2) 的 `docs/javlibrary-follow-up.md`。该 PR 尚未合并，本文件不把其历史运行器错误当作 NAS 根因。

保留 `New(proxy)`、原始 10 分制、抓取路径、选择器、HTTP/API 响应结构及缓存行为。JavDB/FANZA、自动下载、全局网络配置和发布流程均不变。

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

原访问日志的 `id` 与响应头 `X-Request-Id` 用于定位 API 请求。Go 的重定向错误可能包含相对 Location 或畸形响应头，原访问日志的绝对 URL 脱敏不能覆盖所有情况，因此此模块仅打印允许的 cause 诊断字段，原始错误对象仍通过 Unwrap 保留。主机/IP/路径仍可能存在，分享前替换私人地址。不要分享完整代理配置或登录 Cookie。

## 飞牛最短验证

1. 记录部署版本。打开 Miyabi 网络代理设置，确认已启用、已保存，刷新后仍保持。`GET /api/settings/network` 可核对已保存配置，但其中地址可能含凭据，只记录脱敏结果。设置页“测试连接”只测试 JavDB/JavBus，也可使用未保存输入，不能证明 JavLibrary 已走代理。
2. 使用已登录的同源浏览器请求 `/api/extensions/movie/javlibrary-rating?code=<实际番号>`，再请求 `/api/extensions/movie/comments?source=javlibrary&code=<实际番号>&page=1`；返回 has_more 时检查第 2 页。记录时间、状态、`X-Request-Id`。选近期未查询的已知影片，或等待已有影片缓存自然过期，不清缓存、不修改代理来制造故障。
3. Docker 安装读取 `docker logs --since 10m --timestamps '<实际容器名>'`；原生安装读取 Miyabi 进程对应日志。通过请求 ID 找到单条失败，检查 operation/category/reason 与代理/HTTP 状态。入口反代返回 HTML 502 且无对应应用日志时，先查入口和进程。
4. 用同一时间窗口的代理连接/流量记录交叉验证 Miyabi 到 `www.javlibrary.com:443` 的访问；复用连接未必产生新 CONNECT。应用日志 proxy=direct 仅指未使用应用内显式代理，不排除宿主机透明代理。

影片查找仍缓存 10 分钟、评论 2 分钟，失败不缓存。直接扩展接口避开列表额外持久缓存，但仍会使用影片/评论缓存。`Cache-Control: no-store` 和浏览器刷新不绕过这些服务端缓存。默认日志没有出站成功摘要；如需观察正常空结果，后续经用户安排启用 debug，或取得同网络路径的脱敏 HTML，不为诊断新增清缓存接口。

## 保留的证据边界

- 分制：参考 [JAVMovieScraper 的 JavLibrary 适配器](https://github.com/DoctorD1501/JAVMovieScraper/blob/2099948d716a61166bd4a4ae0bfffc43b4e77f18/src/main/java/moviescraper/doctord/controller/siteparsingprofile/specific/JavLibraryParsingProfile.java#L161) 明确按 `Rating(10, ratingText)` 读取原始 `.score`。本轮无法取得真实站点页面，不能宣称重新完成当前线上分制或选择器验收；不进行五分换算。
- `.videos` 内部变化、已有 `#video_review` 中缺少 `.score`、已有评论容器中的字段变化，仍可能与合法空结果无法区分。新增 shape 供比对，未擅自改变这些语义。
- 评论重定向仍只校验影片 v；最终页码回退待真实响应确认。没有修改分页、去重或选择器。
- 重定向策略未放宽：`len(via) >= 3` 在第三跳拒绝，即最多跟随两跳。原详情文档的“三次”已按实现纠正。
- 运行器旧 DNS 错误和 195 字节 Site Unavailable 页面不能证明 NAS、源站或 Cloudflare 的状态。没有在线评分、评论分页、NAS 或浏览器验收结果。

## 验证状态

本轮仅运行已有 gofmt 的格式化/语法解析与 `git diff --check`，不安装依赖、不运行 go test/go vet/go build，不触发发布工作流。新增测试源码尚未编译或执行，不记为通过。

已准备的回归用例：错误类型与取消链保留；相对 Location/畸形响应头错误不泄漏原文；实际代理动态切换及 CONNECT 407/403；HTTP/异常页/未知 HTML/超限区分；两跳通过、第三跳拒绝；真正返回错误影片的身份保护；合法空结果和评论词语不误判；debug 摘要不含敏感文本；真实适配器失败经 API 返回通用 502、访问日志脱敏且保留请求 ID。

恢复测试后，先运行 `go test ./internal/javlibrary ./internal/catalogue ./internal/api`，并对 `internal/javlibrary` 的回调/取消路径运行 race 检测；通过后再开展 NAS 真实响应验收。当前保持 Draft，不合并、不部署。
