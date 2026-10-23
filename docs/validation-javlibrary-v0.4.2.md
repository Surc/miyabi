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

发布工作流基于合并后的 master 执行前端与 Go 生产构建、真实程序/鉴权/网页/原生依赖 smoke、FPK preview 打包和 linux/amd64 两镜像推送，全部成功后公开 Release。FPK 仍未完成 J4125/fnOS 安装、升级、ACL、字体/TLS 和 Emby 实机验收。

真实 JavLibrary 评分、评论和分页的在线响应尚未验收。本版只改善失败可诊断性，未修改来源路径、评分选择器、分制、合法空结果或缓存策略，不宣称解决第三方来源可用性。
