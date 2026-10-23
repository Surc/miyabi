# 扩展回归验证（2026-10-23）

用户本轮要求“测试一下”，恢复测试及其必要的测试编译；正式产物构建、FPK 打包、合并、发布和 NAS 部署仍暂停。本轮验证基线为 PR #7 `e4a11dc4e6c71c88f8470db7a02ad1a807452e06`，包含播放 PR #6 的审核修复，另新增可复现的 MDCz runtime 用例与检查配置。期间用户追加入库版本/移除整体加载区要求，本轮已实现并补测，见 [卡片扩展](library-card-extensions.md)。

## 已完成项目

| 检查 | 结果与范围 |
| --- | --- |
| 前端 Vitest | 最终 40 文件、251 测试通过，含缓存、字幕筛选、名称、播放、介绍、隐私、入库版本及卡片加载回归 |
| Go | 37 个有测试的包完成验证；全量执行中 36 包直接通过，codeid 默认超时后按语料组补跑通过，详见下文；卡片改动后 library/api 再次通过 |
| 纯 Node | 36 测试通过：前端纯函数 14、桥接 HTTP/配置 11、FPK 配置/生命周期 11 |
| Python | FPK 产物头、图标与包声明 3 测试通过 |
| Go 竞争检测 | catalogue、api、mdcz、library/scrape 通过；卡片改动后补跑 library/api 也通过 |
| MDCz 真实 runtime | 固定引擎上的 4 项集成测试通过，见下节 |
| 真实原生依赖预检 | 当前 Linux x64 / Node 24.19.0 上运行原生包的 native-probe：sharp JPEG、impit 初始化、SQLite 内存库成功 |
| 类型与静态检查 | 前端及桥接 runtime TypeScript noEmit、Oxlint/Oxfmt、shell 语法、diff 检查通过 |

Go 全量 `go test -json ./...` 的 `internal/codeid` 在随机 300 个原型文件名的反向语料测试达到默认 10 分钟超时，没有断言失败，其余 36 个有测试的包通过。随后以 `-timeout 30m` 分别运行最新原型反向、随机原型正向、随机原型反向及排除两组大语料后的其余用例，四组均通过；结合首轮已通过的最新原型正向覆盖全部 codeid 用例，没有减少测试语料。全量输出另有 20 个无测试包、19 个原有跳过测试/子测试事件；不把跳过计为通过。Go 工具链为 1.27.3。

可复现的 codeid 分组命令（首轮最新原型正向已通过）：

```sh
go test -timeout 30m -run '^TestPrototypeLatestFilenames$/^reverse_true$' ./internal/codeid
go test -timeout 30m -run '^TestRandom300PrototypeFilenames$/^reverse_false$' ./internal/codeid
go test -timeout 30m -run '^TestRandom300PrototypeFilenames$/^reverse_true$' ./internal/codeid
go test -timeout 30m -skip '^TestPrototypeLatestFilenames$|^TestRandom300PrototypeFilenames$' ./internal/codeid
```

## MDCz runtime 验证与复现

此前的纯 Node 测试使用模拟 adapter，不能证明实际 MDCz 接口/字段匹配。本轮新增 `extensions/mdcz-bridge/runtime.test.ts`，直接加载固定 MDCz `f65e7d4161151aff1531f3b99872e49234d5d539` 的配置 schema、来源注册表、聚合/发布、文件名识别和角标实现：

1. 真实默认配置初始化，网页管理配置保存、凭据脱敏、重新加载。
2. sharp 实际合成中字/破解/1080P 海报，多分集只保留共同标签，无标签保留原图，取消后清理临时目录。
3. impit 请求本地 HTTP 图片夹具，再经 sharp 输出 JPEG。
4. 仅模拟爬虫响应，使用真实聚合和发布链路验证番号、标题、介绍、时长与不透明图片引用；不伪造聚合评分分制。

没有请求真实影片站点、翻译服务或使用用户账号。第四项不代表站点解析或翻译账号已验收；第三项只验证本地 HTTP，不代表公网 TLS、代理和 Cookie 已验证。

在固定 MDCz checkout 内执行（`/path/to/miyabi` 替换为本分支路径；目标 `apps/miyabi-bridge` 应为不存在的临时目录）：

```sh
CI=true ELECTRON_SKIP_BINARY_DOWNLOAD=1 SKIP_INSTALL_SIMPLE_GIT_HOOKS=1 \
  corepack pnpm@10.28.2 --filter @mdcz/server... install --frozen-lockfile
cp -R /path/to/miyabi/extensions/mdcz-bridge apps/miyabi-bridge
ln -s ../server/node_modules apps/miyabi-bridge/node_modules
node_modules/.bin/vitest run --config apps/miyabi-bridge/runtime.vitest.config.ts
node_modules/.bin/tsc --noEmit -p apps/miyabi-bridge/tsconfig.runtime.json
```

测试复用生产 Vite 的 alias，启动时核对引擎 Git SHA。依赖来自上游冻结锁文件，不新增 Miyabi 依赖声明；桥接测试目录复用 server 的依赖，不能假设 pnpm 会把 sibling 依赖提升到根目录。类型检查显式包含上游 vendor 声明，无新增 ambient 类型替代上游声明。安装时跳过 Electron 二进制下载和 Git hooks；本轮没有生成 bridge.js 或安装包。

常规检查命令：

```sh
go test -json ./...
go test -race ./internal/catalogue ./internal/api ./internal/mdcz ./internal/library ./internal/library/scrape
# 在 web 目录执行
node_modules/.bin/vitest run
node_modules/.bin/tsc --noEmit
# 返回仓库根目录
node --test extensions/mdcz-bridge/*.test.mjs packaging/fnos/tests/*.test.mjs web/tests/*.node.mjs
PYTHONDONTWRITEBYTECODE=1 python -m unittest discover -s packaging/fnos/tests -p '*_test.py' -v
```

## 尚未完成的验收

- FPK 生命周期测试仍使用模拟 Miyabi/桥接；运行器 PID 与 procfs 不一致时仅替换临时测试包的身份适配器，生产 Linux boot ID/start ticks 实现另有单测。
- 当前机器原生依赖通过不等于 J4125/fnOS 的 CPU、GLIBC、字体、TLS 和 ACL 兼容性通过；最低飞牛版本仍待实测。
- 未执行生产 Go/前端/桥接构建、Docker 构建、完整 prepare 或 fnpack，没有可安装 FPK。未验证 NAS 安装、升级、卸载、共享目录保留和旧 Docker 迁移。
- 浏览器操作、真实站点评分/评论、外部翻译、Emby 直接播放和封面刷新仍需用户环境验收；PR 继续保持 Draft。
