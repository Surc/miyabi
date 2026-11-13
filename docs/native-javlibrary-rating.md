# 原生 JavLibrary 评分

Miyabi 默认评分客户端采用 Amane 同类的浏览器指纹请求方式，在现有 Go 程序内运行，不需要部署 Amane、Python 服务或安装新依赖。参考的是 [Amane v0.23.0 的网络层](https://github.com/sqzw-x/amane/blob/323c97e1a5fc1efe93fb39a6f1f50a37d5ec294b/src/amane/net/http.py)和 [JavLibrary 爬虫](https://github.com/sqzw-x/amane/blob/323c97e1a5fc1efe93fb39a6f1f50a37d5ec294b/src/amane/crawlers/sites/javlibrary.py)；本实现不是调用其 Python 代码，也不包含其浏览器渲染后端。

## 行为

- 使用 Miyabi 已保存的应用代理；运行中更改代理会在下一次实际请求时生效。不会读取系统代理环境变量，也不会把某台 NAS 的代理地址写死在程序里。
- 使用现有 `tls-client` 的 Chrome 124 / 131、Firefox 133 / 135 指纹和对应 User-Agent，保留搜索、详情及同源跳转之间的 Cookie 会话。
- HTTP 403/406 时最多尝试四种指纹；为同一代理保留已选指纹，全部被拒后不在每次请求中重跑整组。切换指纹或代理会重建 Cookie 会话。其他 HTTP 错误不会触发指纹切换。
- 同一评分客户端限速每秒五次、突发一条；等待限速、指纹切换和同源跳转都计入每次 HTTP 操作的 15 秒超时。
- 搜索兼容旧 `?v=` 和新 `/cn/<影片 ID>.html` 链接，详情跳转允许这两种地址之间规范化；完整番号仍须匹配，路径与显式 `v` 身份冲突时拒绝。从 `#video_review .score` 提取原始 10 分制评分；拒绝错片、越界分数、验证页和异常页面。没有评分保持空值，不补零分。
- 不请求封面、视频、评论或其他来源。JavLibrary 评论入口继续停用，JavDB 评论和其他评分不变。

详情和发现卡片共用 `MovieExtras` 的独立评分缓存，失败不缓存；发现列表原有持久缓存和过期时间继续沿用。无需重刮、迁移数据库或重建 STRM。

如果已显式启用 `MIYABI_JAVINIZER_CONFIG`，仍使用原来的 Javinizer 评分服务，不会自动在两个引擎之间切换。没有启用时使用本原生客户端。底层旧 `javlibrary.New` 保留原行为，供现有评论客户端和诊断使用。

## 边界与验证

此移植没有 Amane 的随机初始指纹、全部重试策略、浏览器渲染或真人验证会话导入功能。站点要求验证时仍会返回来源异常；代码接入成功不等于真实站点评分已恢复。HTTP 403 等状态仍使用既有脱敏诊断；指纹库不提供原标准传输的 CONNECT 状态回调，日志中的 `proxy_status=0` 表示未观测到该值。

本地测试运行真实的 Go 指纹客户端和回环 HTTP 站点，检查评分、错片、Cookie、跳转边界、指纹切换上限、代理热切换和取消。它们使用合成 HTML，不是线上页面证据。浏览器验证仍由用户完成。

2026-10-29 在用户电脑的 WSL Ubuntu 中运行本原生客户端，经 `http://192.168.1.6:7890` 请求 IPX-535、ABP-123，最终均为 HTTP 403，未取得真实评分。第一条请求完成受限指纹切换，第二条沿用最后的会话；没有运行浏览器、Amane Python 或修改飞牛部署。`internal/javlibrary` 本地测试及 `internal/javlibrary`、`internal/catalogue`、`internal/api`、`internal/app` 的 race 测试均通过。

2026-10-31 通过飞牛实际评分 API 复测 FSDSS-437 和 IPZZ-982，两者均返回 502；对应日志为搜索阶段 HTTP 403，使用飞牛当时已保存的应用代理。沿同一代理独立请求中文、日文、首页及用户给出的静态详情页均收到 `Cf-Mitigated: challenge`；现有依赖中的 Chrome 144/146 指纹对照也仍为 403，不能把更换指纹当作恢复方案。

用户手动打开的真实页面经只读 DOM 检查确认：`javme4ivte.html` 对应 FSDSS-437，评分为 `(8.40)`；`javme33a5a.html` 对应 IPZZ-982，存在完整 `video_info` 与 `video_reviews`，但没有 `video_review`。据此补齐静态详情链接、旧地址规范化跳转，以及完整未评分页面的兼容。最小化 DOM 摘录保存在 `internal/javlibrary/testdata/`，不含 Cookie、广告、用户信息或评论正文；本地回放验证这些节点与身份边界，不代表取得了飞牛 HTTP 链路的正常页面。

同工作区新增的可选浏览器会话通道，其 Go 和 Node 端目标校验也兼容上述静态详情地址；只放行固定 JavLibrary 中文路径，路径 ID 与显式 `v` 冲突、重复参数、其他站点和不相关 HTML 路径继续拒绝。新增本地用例回放原生 HTTP 与浏览器获取接口两条链路的已评分/未评分页面及跳转。页面解析修复通过 JavLibrary、catalogue、API、app 四包常规和 race 回归；随后补充的会话通道页面兼容用例也单独通过 Go race 与 Node 测试，不将这些结果视为并行开发中的整个会话通道已经验收。浏览器获取接口测试使用本地 HTTP 模拟服务，不启动浏览器。

页面兼容修复完成时尚未部署飞牛，也没有解决原 HTTP 客户端的 Cloudflare 验证拦截。IPZZ-982 的合法无评分与飞牛的访问失败是两个独立状态；不能将 403 改为空成功，也不能把浏览器中的 FSDSS-437 分数写死或注入评分缓存。后续评分会话通道的部署与实测结果见本文末节。

```sh
go test -tags dev ./internal/javlibrary ./internal/catalogue ./internal/api ./internal/app
go test -tags dev -race ./internal/javlibrary ./internal/catalogue ./internal/api ./internal/app
```

## 2026-10-31 线上复核

- 飞牛运行的是包含 `internal/javlibrary/rating.go` 的 0.4.8 安装包。直接调用其评分接口，IPX-535 在搜索阶段返回上游 HTTP 403；脱敏日志确认实际使用已保存的 PC 代理。尚未取得影片 HTML，评分解析和缓存均未进入成功路径。
- 在 PC 上使用同一代理服务的本机入口 `http://127.0.0.1:7890`，按原生实现的四组指纹、请求头和 Cookie 隔离方式逐一请求 IPX-535。Chrome 124 / 131、Firefox 133 / 135 全部返回 `403`、`Server: cloudflare`、`cf-mitigated: challenge`；正文含 `_cf_chl_opt`，没有影片身份节点。另用普通 HTTP 请求 ABP-123，结果相同。
- [`cf-mitigated: challenge` 是 Cloudflare 明确的验证页标记](https://developers.cloudflare.com/cloudflare-challenges/challenge-types/challenge-pages/detect-response/)。这次失败发生在页面获取阶段，不能归因为没有评分、解析选择器失效，或未安装移植代码。
- 所参考的 Amane 版本另有可选浏览器后端，执行页面脚本、按来源复用浏览器上下文，并等待验证页消失；超时仍会返回验证未解决。当前 Go 移植没有这部分能力，不能把 HTTP 指纹移植视为完整的浏览器请求流程。参考 [Amane 浏览器后端](https://github.com/sqzw-x/amane/blob/323c97e1a5fc1efe93fb39a6f1f50a37d5ec294b/src/amane/net/browser.py)。
- 当前 `Client.page` 在非 200 响应时直接返回错误，所以原日志的 `bytes=0` 表示没有读取错误页正文，不表示上游响应为空。此次只读探针读取了验证页标记；没有运行浏览器、安装依赖、更改生产配置或生成新的 FPK。

当时确认需要新增可选的浏览器页面获取与会话复用通道，再交给现有 Go 代码核对番号和提取评分。人工验证仍由用户完成，也不能保证所有节点和会话都能通过；仅扩大 HTTP 指纹轮换不能解决本次已复现的验证要求。

## 可选的人工验证会话通道

`tools/javlibrary-browser/start.mjs` 使用已安装的 Node 和 Edge，在独立用户目录保留人工验证后的浏览器会话，不安装浏览器或第三方依赖。控制页仅在本机开放；飞牛通过独立凭据访问取页接口，凭据不发送给 JavLibrary 或前端。启用时浏览器按网站正常加载页面，Go 继续负责完整番号、最终 URL、原始分数和缓存校验。

浏览器调试仅监听回环地址，默认使用明确的非零端口 `18083`；端口 `0` 会启用 Chromium 的自动化标记。重连使用专用目录内记录的浏览器端点，不接管普通 Edge 的用户目录。人工验证阶段允许同源挑战跳转；自动取分继续限定中文搜索与已识别影片地址，不能以人工验证放行扩大评分接口的目标范围。

飞牛原生配置可设置 `javlibraryBrowser: {serverURL, apiToken}`，由原生服务写入权限受限的 `javlibrary-browser.json`，并仅向 Miyabi 传递 `MIYABI_JAVLIBRARY_BROWSER_CONFIG`。单独运行时也可直接通过该环境变量指定连接文件。已经启用 Javinizer 时仍优先使用 Javinizer。

本次 PC 连接页为 `http://127.0.0.1:18082/`。用户点击“打开验证浏览器”，在专用 Edge 完成人工验证，再点击“验证完成”。请保持 PC、专用窗口及对应代理运行；浏览器再次要求验证时，评分请求返回可重试的来源错误，等待人工验证，不缓存该失败。LAN 取页接口仅绑定已配置的 PC 地址并校验指定 NAS 来源；本机控制和评分接口使用不同的访问限制。

### 由验证服务主动连接 Miyabi

在飞牛的 `native-v1.json` 中将 `javlibraryBrowser` 配置为 `{ "connectBack": true, "apiToken": "原有的连接密钥" }`，重启应用。独立部署可在 `MIYABI_JAVLIBRARY_BROWSER_CONFIG` 指向的文件中使用 `{ "enabled": true, "connect_back": true, "api_token": "同一个连接密钥" }`。

在 PC 原有的验证服务配置中添加 `"miyabi_url": "http://NAS地址:Miyabi端口"`，沿用原来的 `api_token`、Edge 配置、代理和用户目录。启动命令与本机验证页面照旧。验证服务通过 Miyabi 的网页端口主动连接并自动重连，本机操作页仅监听回环地址。

在同一台电脑上通过同一个 NAS 地址直连打开 Miyabi。遇到评分缓存未命中时，Miyabi 根据评分请求和验证连接的来源 IP 匹配电脑。电脑局域网地址改变后，验证服务重连即可。经反向代理或 NAT 访问时，Miyabi 可能只看到代理或网关的 IP，此模式适用于来源地址可直接区分的连接。

## 0.4.10 部署与真实评分验收

飞牛原生安装已从 0.4.9 升级为 0.4.10，基于原 0.4.9 的隔离源码叠加本次评分修复及会话通道，未纳入共享工作区其他尚在开发的功能。升级包完成文件哈希核验与实际打包程序启动检查；安装后二进制 SHA-256 为 `283a4b269068877e27aa70a41f404109d5c0b401366ee8e6394c29cc321f4b5f`，与验证过的包一致。原安装设置、外部 MDCz 连接和登录校验通过。

用户在 PC 完成验证并保持专用 Edge 开启后，2026-11-01 00:06:40（UTC+8）从飞牛实际请求 `/api/extensions/movie/javlibrary-rating?code=FSDSS-437`，得到 HTTP 200、`source=javlibrary`、`found=true`、`score=8.4`、`max=10`；请求 ID 为 `javlibrary-0410-e4ac461fb43246cca3022da7f5c0e143`，会话状态为 `ready`。该请求走真实会话取页与 Go 身份/评分解析，未向生产缓存注入摘录分数。

本次验收覆盖当前 PC、代理和会话下的 FSDSS-437 评分链路。专用 Edge 关闭时会话会断开，评分接口明确返回需要人工验证；重新打开并确认后可继续读取。评论与其他网络出口仍按各自验收记录处理。
