# JavLibrary 诊断回归（2026-10-24）

基线为 `Surc/miyabi:master@e00d4e233164b7e7c4f988d242ee93cb0f4278de`（v0.4.1）。PR #10 初始实现 `c1809d2` 已在隔离工作区与当前主干合并后验证，无冲突。用户本轮授权必要测试、合并及发布，不包含 NAS 部署。

## 已执行

| 检查 | 结果 |
| --- | --- |
| Go 1.27.3：JavLibrary、netx、catalogue、API 四包 | 通过，414 个测试/子测试通过事件，无失败或跳过 |
| 同四包 race 检测 | 通过，414 个测试/子测试通过事件，无竞态报告 |
| 修改 Go 文件 gofmt | 通过 |
| `git diff --check` | 通过 |

复现：

```sh
go test ./internal/javlibrary ./internal/netx ./internal/catalogue ./internal/api
go test -race ./internal/javlibrary ./internal/netx ./internal/catalogue ./internal/api
git diff --check
```

首次执行只有新 API 测试的 ID 回显假设失败。已核对固定 `slog-gin@v1.21.1`：有入站 ID 时日志沿用它，不额外写响应头；无入站 ID 时生成并返回 ID。夹具现分别覆盖这两个场景，保留通用 502、单条错误日志、请求关联及敏感信息不泄漏断言；生产日志中间件未修改。修正后重新运行上述完整四包及 race，均通过。

新增用例覆盖 DNS/代理/TLS/超时/取消分类与原始错误链、CONNECT 407/403 与实际代理切换、非 200/明确异常页/未知 HTML/正文超限、两跳通过与第三跳拒绝、返回错误影片的身份保护、合法空结果、评论词语不误判、相对 Location/畸形响应头错误文本与 debug 摘要脱敏，以及 API 通用响应和请求 ID。

## 发布和证据范围

本轮生产改动仅位于独立 JavLibrary 适配器。前端、公共 netx、JavDB/FANZA、缓存与下载逻辑未修改；不重复运行未变化的前端功能、大语料 codeid 或 MDCz runtime 回归，也不将历史结果记作本轮重跑。

[PR #10](https://github.com/Surc/miyabi/pull/10) 已合入 `9570c514a36458c71ca00a036d3214778d22a405`。[发布运行 40028000713](https://github.com/Surc/miyabi/actions/runs/40028000713) 基于该提交执行前端与 Go 生产构建、真实程序/鉴权/网页/原生依赖 smoke、FPK preview 打包和 linux/amd64 两镜像推送，已完成且结论为 success；[v0.4.2 Release](https://github.com/Surc/miyabi/releases/tag/v0.4.2) 于北京时间 2026-10-24 05:38:55 公开。FPK 仍未完成 J4125/fnOS 安装、升级、ACL、字体/TLS 和 Emby 实机验收。

真实 JavLibrary 评分、评论和分页的在线响应尚未验收。本版只改善失败可诊断性，未修改来源路径、评分选择器、分制、合法空结果或缓存策略，不宣称解决第三方来源可用性。

## 2026-10-24 文档归并

[PR #2](https://github.com/Surc/miyabi/pull/2) 只包含历史问题文档，不需要单独合并。其 JL-01–JL-04、后续顺序和完成条件已逐项对照 PR #10；已实现诊断与仍待完成的真实来源验证统一保存在 [诊断文档](javlibrary-diagnostics.md#pr-2-逐项对照)，后续只更新其中的 [真实来源与 NAS 验收清单](javlibrary-diagnostics.md#真实来源与-nas-验收)。本回归记录不另建问题清单。

本次归并仅修改文档，核对了 PR diff、master、Release 和发布运行；执行文档差异、引用与范围检查。没有重新运行业务测试或真实在线验收，没有修改业务代码、标签或重新发布 v0.4.2。上文 Go/race 数字仍是诊断实现当时的回归证据。

## 后续响应校验修复（2026-10-24，未发布）

用户随后要求“修复”，基线为 master `a5ed66c36b7e134ad9c73ef421d49ac994b68a76`。本节记录后续代码回归，不改写 v0.4.2 的发布证据，也不表示新修复已发布或部署。

先以本地 HTTP 夹具复现旧实现接受评论页码回退/缺失、搜索候选身份缺失误报未收录、评论行缺少正文误报空成功，以及单侧括号评分被接受的问题；新增专项测试在旧实现上失败。修复只调整 `internal/javlibrary/client.go`、`parser.go`，保持原始 10 分制、旧 API、公共 netx、缓存生产逻辑及前端。另补充纯查询下一页、数字等值页码、重复参数、错误端点和海报/标题共用影片 ID 的正反例。

| 检查 | 本次结果 |
| --- | --- |
| Go 1.27.3：JavLibrary、netx、catalogue、API 四包 | 全部通过，440 个测试/子测试通过事件，无失败或跳过 |
| 同四包 race 检测 | 全部通过，440 个测试/子测试通过事件，无失败、跳过或竞态报告 |
| Node 独立逻辑 | 14 项通过，无失败或跳过 |
| 修改 Go 文件 gofmt、`git diff --check`、文档引用/锚点及范围检查 | 通过 |

新增 catalogue 回归确认：第 2 页来源失败不缓存，重复调用仍请求来源；第 1 页成功缓存保留；来源恢复后可重试并缓存成功的第 2 页，共享影片查找不重复。已有前端回归中的评论去重、失败保留已加载内容继续由四包之外的原验证记录说明；本次没有重跑 Vitest、类型检查、前端生产构建、大语料 codeid 或 MDCz runtime，不把历史结果计入本轮。

复现命令：

```sh
go test ./internal/javlibrary ./internal/netx ./internal/catalogue ./internal/api
go test -race ./internal/javlibrary ./internal/netx ./internal/catalogue ./internal/api
node --test web/tests/*.node.mjs
git diff --check
```

运行器当天的一次 urllib 来源探测仍为 HTTP 200、195 字节 `Site Unavailable`，无正常列表/详情/评论标记；没有使用用户 NAS 实际应用内代理，不能作为正常在线来源或 NAS 验收。真实评分、评论分页和部署路径仍未完成，继续只维护 [诊断文档的 A1–A5](javlibrary-diagnostics.md#真实来源与-nas-验收)。本轮未合并、打标签、重新发布 v0.4.2 或部署 NAS。
