# Emby 直接播放扩展

本次基于 fork `Surc/miyabi:master@7fbe205e484f829f9a4c6183025bfae3a1e45274`，分支 `feat/emby-direct-play`。2026-10-23 已读取最新上游 `ppxb/miyabi:master@d3c86548b477ed2d84eb46bef92dd25f64b4e566`：相对原基线 `f1903459b45dece1498c05c7b309773776c756e9` 仅有 README 环境变量和 Compose 升级说明，涉及的生产源码无变化。本次不夹带该文档更新。

## 使用方式与支持范围

1. 在 Miyabi 的 Emby 设置中启用集成，填写 Emby 服务器地址、API Key、导出目录和 Emby 端对应媒体库路径。路径映射必须与 Emby 实际扫描的 STRM 路径一致。
2. 完成影片入库、导出 STRM 和 Emby 媒体库扫描。
3. 安卓设备安装 **Emby App 3.4.26 或更新版本**，并先登录同一 Emby 服务器。
4. 打开 Miyabi 作品详情，等待“在 Emby App 中播放”就绪后点击，浏览器唤起 App 并提交直接开播意图。浏览器可能先要求允许打开外部应用。

依据 [Emby Android 官方更新记录](https://emby.media/community/topic/95467-emby-for-android-release-notes/)，3.4.26 新增 `emby://play/{serverID}/{ItemID}`。这里使用 `play` 协议；`items` 是详情入口，不能证明会直接开播。其他平台显示版本与平台说明，不发起播放目标请求；尚未实现 iOS、桌面或 Emby Web 的直接开播适配。

App 登录态、客户端版本、访问服务器的网络和播放权限由 Emby 负责；Miyabi 不传入账号、密码或 API Key。Miyabi 不能观测外部 App 是否成功启动，实际开播与续播行为需在用户设备验收。多文件影片默认选择首个生成的视频文件，不保证自动连续播放全部分集。

`server_url` 是 Miyabi 后端访问 Emby 的地址，允许已有 `/emby` 前缀；生成的协议只包含服务器 ID 和影片 ID。现有 `public_url` 仍指 **Miyabi 的 STRM 访问地址**，不要将它改作 Emby Web 地址。此功能只支持 Miyabi 当前导出目录结构，不以番号模糊查找任意其他来源媒体库。

## 独立接口与行为

```text
GET /api/extensions/emby/playback?code=ABC-123
```

路由挂在原鉴权与安全中间件内，响应 `no-store`。GET 只查询目标，不在服务器上发起播放。没有新设置字段，不改变原 `EmbyManager` 或旧 API。

| status | 含义 |
| --- | --- |
| `ready` | 返回 `url: "emby://play/<serverID>/<itemID>"` |
| `disabled` | 尚未启用或缺少服务器/API Key |
| `not_exported` | 当前影片没有符合生成命名规则的本地 STRM |
| `not_found` | Emby 明确返回匹配路径的空列表，需要扫描媒体库 |

参数错误、目录读取错误、网络/鉴权失败、响应格式变化、路径不符、目标歧义均保留为错误，不伪装成未入库。取消沿用原上下文语义。当前详情可手动“重新检查”。

新服务从 `export.EmbyMovieDir` 和 `nfo.FileStem` 定位生成文件，排除无关名称、目录和符号链接；多文件按 cd 数字顺序选择。复用 `translatePath` 映射 Emby 路径，使用 [官方 GET /Items 的 Path 参数](https://dev.emby.media/reference/RestAPI/ItemsService/getItems.html) 精确查询，并校验返回 Path/Id。确认唯一影片后读取 `/System/Info` 的服务器 ID。链接中不包含 STRM 地址或令牌，API Key 只放后端请求头，扩展请求禁止跨源重定向。

前端只为安卓当前详情查询，30 秒短缓存，关闭详情时可取消；列表预加载不访问 Emby。修改 Emby 设置后使播放查询失效。目标提前解析为普通链接，点击时同步打开协议，避免等待异步查找后丢失浏览器用户手势。

## 模块与升级接入点

| 新增模块 | 职责 |
| --- | --- |
| `internal/emby/playback_extension.go` | 复用配置快照/路径映射/HTTP 客户端，增加只读目标解析方法 |
| `internal/api/emby_playback_extension.go` | 可选窄接口、独立路由与参数验证 |
| `web/src/api/emby-playback.ts` | 独立缓存 keys、取消、协议校验 |
| `web/src/features/movie-detail/extensions/emby-playback.tsx` | 平台适配、播放入口、不可用与重试状态 |
| `web/src/components/movie/display-title.ts` | 独立作品展示名称函数 |

原文件仅在 `app.go` 注入依赖、`router.go` 注册能力；`hero.tsx` 新增可选 `actions` 插槽，`content.tsx` 挂载扩展；`api/emby.ts` 在设置保存后使独立查询失效。列表卡片与 Hero 使用展示名称函数。升级时先核对导出目录/文件命名、路径映射、配置快照、HTTP 请求约定及 Emby Android 协议，再调整新增适配器；不向旧接口添加强制方法。

## 本次一并落地的既有要求

- 列表与详情统一显示“番号 · 名称”，相同番号避免重复，无番号保留原标题。只改变展示，不写回来源资料、NFO、文件名或旧 API。**已有资料无需重刮或重建**，升级页面即应用。
- 发布工作流仅保留 `linux/amd64`，适合用户 J4125；移除 ARM64 和 QEMU。仍只由版本标签触发，功能分支推送不发布镜像，历史 v0.3.0 标签与镜像不变。

## 验证与验收

2026-10-23 在用户追加“先不编译”前，已通过 Emby/API 两包 Go 专项回归、前端完整 Vitest 37 文件/238 测试、TypeScript、Oxlint、Oxfmt 和前端生产构建。专项回归覆盖精确路径、错误目标、多个目标、空结果、取消、重定向、分集顺序、鉴权、链接/按钮状态及番号展示，使用本地 HTTP fixtures 和 SSR，无需真实媒体或账号。

收到“先不编译”后不再运行编译、打包或发布。之后只完成 Go 格式检查、diff 空白检查和工作流 YAML/amd64 目标/仅标签触发的静态核对。随后补充的 `TestPlaybackServerFailuresDoNotBecomeNotFoundOrLaunchLinks` 四个子场景尚未执行；完整 Go 回归、race、服务端可执行文件与镜像构建也未执行，恢复编译授权后再补。前述通过结果不能覆盖这些未执行项。本次没有修改依赖声明、锁文件或 README。

浏览器及真实 Emby App 验收由用户完成：确认正常影片点击后直接进入播放、未扫描时给出提示、服务器配置变更后链接更新、多文件从首文件开始、App 未安装或未登录时的系统表现。尚未完成这些检查，不能将协议与 fixture 测试写成实机开播已通过。
