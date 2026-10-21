# JavLibrary 问题记录与后续修复

## 范围与基线

记录 2026-10-21 的 JavLibrary 排查结果，供后续修复与上游升级使用。当前实现已随 PR #1 合并，核对基线为 `24db1ab1b98b6c4150daf7b9694ff0a31b0789d0`。本文档 是排查与待验收清单，不代表已确定用户部署端的故障根因。

实现说明见 [详情页扩展维护文档](https://github.com/Surc/miyabi/blob/24db1ab1b98b6c4150daf7b9694ff0a31b0789d0/docs/movie-detail-extensions.md)。

## 已确认的事实与限制

| 编号 | 状态 | 证据与影响 |
| --- | --- | --- |
| JL-01 | 运行器网络阻塞，部署端原因待确认 | 使用实际 `javlibrary.New(nil).Lookup` 请求时，本地 DNS 的 UDP 连接报 `connect: network is unreachable`；请求未进入 HTTP 响应及页面解析。这只能说明当前运行器的直连限制，不能据此判定 NAS 配置或源站故障。 |
| JL-02 | 代理配置行为已确认，部署配置待核对 | JavLibrary 已接入 Miyabi 的 `ProxyManager`；`netx.NewTransport` 逐请求读取应用内配置，不读取 `HTTP_PROXY/HTTPS_PROXY`。仅给 Docker 设置环境代理并不代表扩展走代理；未启用应用内代理时会直连。这是现有网络约定，不应直接当作代码缺陷修改。 |
| JL-03 | 真实页面验收尚未完成 | curl/urllib 经运行器环境代理得到 HTTP 200、195 字节的 `Site Unavailable` 页面，没有观察到 Cloudflare 挑战特征。该结果无法验证评分、影片匹配或评论选择器，也不能证明源站故障或 Cloudflare 拦截。 |
| JL-04 | 错误可诊断性待改善 | 扩展 API 对外统一返回来源不可用的 502 提示；后续需核对现有日志是否足以区分 DNS、代理、超时、HTTP、拦截页面及结构变化，再补充必要的脱敏诊断信息。不要直接公开底层错误或代理凭据。 |

## MDCx / MDCz 源码对照

- **MDCx**：`sqzw-x/mdcx@58e3f930f2e864fceb8a53ceef818716e2a6413d`（核对时已归档）。
  - [入口 javlibrary_new.py](https://github.com/sqzw-x/mdcx/blob/58e3f930f2e864fceb8a53ceef818716e2a6413d/mdcx/crawlers/javlibrary_new.py) 先用日文站定位影片，再按配置获取中文页面。
  - [javlibrary.py](https://github.com/sqzw-x/mdcx/blob/58e3f930f2e864fceb8a53ceef818716e2a6413d/mdcx/crawlers/javlibrary.py) 用 `vl_searchbyid.php?keyword=` 搜索，结合详情标题或搜索链接的 `title` 匹配番号，多结果优先非蓝光标题。评分仍读取 `#video_review .score`，与 Miyabi 当前适配器一致；该文件没有评论抓取实现。
  - [公共请求层](https://github.com/sqzw-x/mdcx/blob/58e3f930f2e864fceb8a53ceef818716e2a6413d/mdcx/web_async.py) 使用 curl_cffi 浏览器指纹、会话、显式代理、限速及部分状态码重试。支持自定义站点，JavLibrary 的 custom_url 分支会禁用该请求的代理。其实现仍可能报告 Cloudflare 拦截，不能保证套用后即可恢复；不照搬关闭 TLS 校验的设置。
- **MDCz**：`ShotHeadman/mdcz@f65e7d4161151aff1531f3b99872e49234d5d539`。
  - 已全文检索 `javlibrary|javlib`，无命中；来源枚举也未注册 JavLibrary，当前没有可直接移植的 JavLibrary 爬虫。
  - 可参考 [NetworkClient.ts](https://github.com/ShotHeadman/mdcz/blob/f65e7d4161151aff1531f3b99872e49234d5d539/packages/runtime/src/network/NetworkClient.ts) 和 [siteError.ts](https://github.com/ShotHeadman/mdcz/blob/f65e7d4161151aff1531f3b99872e49234d5d539/packages/runtime/src/network/siteError.ts) 的代理、会话、限速及错误分类设计。

上述差异是候选改进方向。尚无正常线上 HTML 证明 Miyabi 的评分选择器错误，也不能声称换成日文站、加入 Cookie 或更换指纹已解决问题。

## 后续处理顺序

- [ ] **P1：收集部署端证据。** 记录 Miyabi 版本、影片番号、失败时间、扩展接口状态、应用内代理是否启用及脱敏错误类别。代理地址隐去凭据，不收集账号 Cookie/令牌。
- [ ] **P1：确认实际网络路径。** 在部署环境通过应用内同一代理验证 DNS、代理连接、TLS、HTTP 状态及正常页面结构；不要用宿主机 curl 成功替代应用实际请求验收。
- [ ] **P2：核对评分和搜索页面。** 保存脱敏真实页面，分别覆盖搜索列表、直接跳转详情、未收录、已收录未评分和多结果。证据支持后再补标题匹配、语言路径或规范跳转的兼容处理，保持完整番号匹配。
- [ ] **P2：单独验收评论。** 核对真实评论字段、空评论、分页、下一页失败重试和去重；不能用 MDCx 的评分实现证明评论解析正确。
- [ ] **P2：评估请求层补强。** 只有实际响应证明有必要时，才增加会话、浏览器指纹、受信站点配置或更清晰的错误分类；保留现有构造函数与旧 API。
- [ ] **P3：补充针对性回归。** 对实际复现问题加入脱敏夹具或本地网络测试，覆盖失败不缓存、取消、代理变更和影片身份校验，更新实现文档及维护 skill。

## 完成条件

- [ ] 在用户实际部署网络上确认可读取至少一部已收录影片的原始 10 分制评分，并独立确认评论及分页。
- [ ] 网络失败、拦截页面和结构变化继续返回来源异常，不能伪装成“未收录”或“暂无评论”。
- [ ] 修复优先限制在 `internal/javlibrary/` 及新增函数/接口内；保留原接口、JavDB/FANZA 评分、自动下载选源及默认磁力顺序。
- [ ] 相关测试通过，并明确区分本地夹具回归、线上来源验收和用户浏览器验收。

## 已有验证与关联待验收项

2026-10-21 排查时已运行并通过：

```sh
go test ./internal/javlibrary ./internal/netx ./internal/catalogue ./internal/api
```

四包通过只证明本地回归，不代表第三方线上可用。JavDB 在线评论及浏览器实际预告播放也仍待验收，属于关联验收缺口，尚未证实为代码故障。预告使用浏览器网络，HLS 依赖原生解码能力。

本记录仅保存已知事实、待验证项和修复顺序，不包含业务代码修复。
