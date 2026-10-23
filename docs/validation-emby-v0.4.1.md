# Emby 播放补丁回归（2026-10-23）

基线 `Surc/miyabi:master@1e142f413de081754613bee567b5024eca2ac084`，四项修复独立于旧 EmbyManager、通知路径映射、STRM 导出和已有配置。用户本轮授权修复、合并和发布，恢复必要测试与构建。

## 已执行

| 检查 | 结果 |
| --- | --- |
| Go `internal/emby`、`internal/api`、`internal/export`、`internal/nfo` | 通过，包含此前四个服务器异常子场景 |
| Go race `internal/emby`、`internal/api` | 通过 |
| 前端 Vitest | 41 文件、258 测试通过 |
| 前端 TypeScript、Oxlint、Oxfmt | 通过 |
| 前端生产构建 | 通过 |

新增 Go 测试覆盖 `/` 映射、盘符/UNC 根与分隔符、大小写保留、百分号字面值、目录外路径拒绝、带末尾斜线的 `/emby` 前缀、非法服务器基址、1 MiB 边界、两个接口的超限响应、异常 JSON、上游错误正文脱敏、同源重定向及循环限制、响应期间取消。既有跨源重定向、鉴权、精确目标、重复目标、无效标识和多文件选择用例保留并通过。

新增前端测试使用真实 QueryClient/QueryObserver 和模拟 HTTP/时钟，覆盖可见页面轮询、后台暂停、回到前台即使数据新鲜也刷新、媒体库扫描后自动发现、刷新失败保留旧数据时仍禁用、卸载取消和空番号无请求；SSR 与真实点击处理器测试覆盖过期/获取中/失败无链接及定时器延迟后的点击拦截。正常点击依然为同步 `<a href="emby://play/…">`，测试不启动真实外部 App。

复现命令：

```sh
go test ./internal/emby ./internal/api ./internal/export ./internal/nfo
go test -race ./internal/emby ./internal/api
cd web
npm run test
npm run lint
npm run fmt:check
npm run build
```

## 范围与实机检查

本轮未重复运行未改变的 codeid 大语料或 MDCz runtime 测试；它们的 v0.4.0 结果见原验证记录。新补丁只对上述已执行范围作通过声明。

发布流程使用合并后的 master，构建 linux/amd64 主镜像、MDCz 桥接镜像和原生 FPK preview，在 native smoke 与产物上传成功后才推镜像、公开 Release。

用户仍需在飞牛 NAS 核实配置根目录与 Emby 实际扫描路径、Linux 目录/文件名大小写、重复条目处理；在 Android 核实 App 版本、服务器登录与播放权限、浏览器外部协议许可、正常单文件及多文件首文件开播。未安装 App、网络不可达和播放/续播策略也依赖实际客户端。不会全局忽略路径大小写或自动选择歧义目标。
