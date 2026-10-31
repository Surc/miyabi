# AVSubtitles 字幕源

AVSubtitles 与迅雷、SubtitleCat 一起注册到默认字幕检索器，沿用现有代理、番号匹配、版本排序、下载失败回退和 Emby 字幕导出流程。无需配置账号或 API Key；一个来源失败不会移除其他来源的结果。

可在「设置 → 字幕下载」分别开关来源、调整简繁偏好并测试真实下载，默认仍全部启用。详见 [字幕下载设置](subtitle-settings.md)。

## 获取流程

1. 请求 `https://www.avsubtitles.com/search_results.php?search=<番号>&language=zh`，仅跟进与番号匹配的作品。
2. 从作品的 `subtitles_list` 表中提取相同作品 ID 的 `/subtitles/zh/` 条目，读取字幕详情中的 `subid`、`revid` 和压缩包文件名；跳过文件名中明确属于其他番号的附件。
3. 选中候选后，通过 `/download_page.php` 建立独立匿名会话，等待站点的 7 秒下载倒计时，再请求该页的下载链接。直接请求 `/download_sub.php` 会进入错误页。
4. 在内存中读取 ZIP 内与详情文件名一致的字幕，沿用原有编码转换和时间轴校验。中文条目未注明简繁时，根据下载文本识别。下载保留现有 URL、重定向和大小检查，解压后的字幕限制为 10 MiB。

搜索总时限为 30 秒，每次查询最多读取 5 个中文字幕详情。每份压缩包选择详情中第一个支持的文本字幕文件；不合并多个分片。页面结构变化、错误页、缺失文件或不支持的压缩格式会使相应候选失败，由既有流程继续尝试其他候选。

搜索和详情页也沿用安全下载的初始 URL、私网、连接固定和重定向校验，响应限制为 5 MiB。详情请求失败会报告错误，同时保留已经找到的可用候选。

## 验证（2026-10-31）

通过实际 Go 实现完成番号搜索、匿名会话下载、ZIP 解压和 UTF-8 规范化：

| 番号 | 下载结果 | 识别语言 | 规范化大小 |
| --- | --- | --- | --- |
| KAGH-050 | SRT | zh-CN | 42,318 字节 |
| SSIS-589 | SRT | zh-TW | 21,121 字节 |

本机代理把站点域名映射为 `198.18.0.73`。已修复此场景：保留原始 Fake-IP DNS、不启用临时 DNS 覆盖，使用真实 Go 实现再次搜索并下载上表两个字幕，结果一致。

## Fake-IP 与下载安全

- 仅系统 DNS 返回 `198.18.0.0/15` 占位地址时，通过固定 HTTPS 端点 `https://dns.alidns.com/resolve` 查询真实 A、AAAA 记录；普通 DNS 继续沿用系统解析，不修改系统配置。
- 系统结果混有其他私网地址时直接拒绝；公共 DNS 返回的所有地址仍须通过原有安全校验，空结果、缺失或异常状态、不完整 JSON、超过 64 KiB 的响应、解析失败或超时也拒绝下载。公共解析总时限为 5 秒，不跟随公共 DNS 接口的重定向。
- 直连前重新解析并验证，连接固定到已检查的真实 IP，保留原始 HTTP Host 与 TLS 主机名。Fake-IP 字面地址、真实私网、回环及其他原本禁止的地址仍不可访问。
- 显式代理仍使用原有代理连接路径；目标 URL 和每次重定向继续校验。原有下载大小、重定向次数和 ZIP 解压限制保持不变。

使用工作区已有 Go 1.27.3 和依赖缓存，关闭依赖下载后通过：

```sh
go test -mod=readonly -tags dev ./internal/subtitle ./internal/domain/subtitle ./internal/netx ./internal/library/scan ./internal/library/scrape ./internal/export ./internal/app
go test -mod=readonly -race ./internal/subtitle
go vet -mod=readonly ./internal/subtitle
```

Fake-IP 修复额外通过以下检查，测试覆盖双栈地址检查、混合公网/私网、公共 DNS 失败、取消、连接固定、DNS 重绑定及代理重定向：

```sh
go test -mod=readonly -tags dev ./internal/netx ./internal/subtitle ./internal/metadata/providers ./internal/network ./internal/app
go test -mod=readonly -race ./internal/netx ./internal/subtitle
go vet -mod=readonly ./internal/netx ./internal/subtitle
```

新增自动测试覆盖番号与中文筛选、匿名会话延续、繁体识别、下载延迟取消、错误下载及解压大小限制，并确认 AVSubtitles 失败时迅雷和 SubtitleCat 仍返回结果。未执行浏览器测试。

2026-11-01 复核时，当前工作区的 `internal/library`、`internal/library/scan`、`internal/library/scrape` 和 `internal/export` 全量测试通过，此前媒体导出并行修改期间的 `TestScanRepairsReportedLetterPartsAndExportsInOrder` 失败不再复现。最新复核与真实 API 下载记录见 [字幕下载设置](subtitle-settings.md)。
