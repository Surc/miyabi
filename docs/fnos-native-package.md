# Miyabi 与 MDCz 原生 FPK

状态：2026-10-23 已按用户“继续完善”完成原生打包层源码，仍暂停编译、打包、合并和发布。没有可安装 FPK，也没有完成飞牛/J4125 实机验证。当前代码是草稿 PR #7 的独立提交；MDCz 功能和限制见 [mdcz-extension.md](mdcz-extension.md)。

## 安装后的使用方式

一个飞牛应用入口，两个业务服务：Miyabi 负责界面、任务、数据和 Emby 导出，MDCz 桥接提供聚合资料、介绍、翻译和图片处理。另有一个只负责两服务生命周期的 Node 管理进程。无需 Docker、Electron 或安装时联网下载依赖；准备产物时仍需完整构建环境。

安装向导只要求网页端口（默认 18080）、Emby 可访问的 Miyabi 根地址、访问密码。访问地址例如 `http://192.168.1.10:18080`，须填写实际可达地址，不能填桥接地址；反向代理用户填写对应公开根地址。网页端口在本版安装时确定，配置向导只更新访问地址和密码，避免改变已导出 STRM 的访问约定。

桥接绑定 `127.0.0.1:3840`，连接密钥自动生成。首次没有保存 MDCz 设置时自动填入连接，但增强开关保持关闭；用户在 Miyabi 设置中启用。已经保存的外部桥接配置不会被覆盖。来源、代理、翻译和角标仍在原来的 Miyabi 设置界面管理。

桌面入口以独立页面打开，保留 Miyabi 自己的访问密码；本版没有接入飞牛统一登录/网关。Emby 仍是已有的独立应用，FPK 只打包对接能力。管理员须将导出目录授权给 Emby；Emby 若运行于 Docker，仍须配置主机目录挂载，并在 Miyabi 中填写 Emby 看到的准确媒体路径。不会为所有用户或任意容器放宽整个应用数据目录权限。

## 包内容和最小接入点

| 路径 | 用途及升级约定 |
| --- | --- |
| `packaging/fnos/manifest.in`、`config/`、`wizard/`、`app/ui/config` | 原生 FPK 元数据、包用户、共享目录、向导与唯一入口；不依赖 Docker 项目资源 |
| `packaging/fnos/app/service/` | 安装配置、预检、进程身份、统一启停/备份；与业务服务通过环境和健康接口协作 |
| `packaging/fnos/cmd/` | 飞牛生命周期的薄入口，转交自有管理脚本 |
| `packaging/fnos/prepare.py` | 从已构建且核验身份的产物准备目录、图标、许可证和校验清单，不编译、不下载、不调用 fnpack |
| `internal/mdcz/bootstrap.go` | 只在用户尚未保存扩展配置时提供初始连接，不开启增强或翻译、不直接修改数据库 |
| `internal/mdcz/service.go` | 唯一既有文件接入：New 在没有已存设置时调用上述可选函数（5 行） |

没有修改旧接口、原生刮削组件、业务依赖/锁文件、README 或发布工作流。上游 Miyabi 升级只需复核 New 的初始设置加载顺序、环境变量和 `/api/auth/config`；MDCz 升级仍集中核对 adapter，另同步包管理器预期的健康协议与引擎 SHA。

## 数据、权限和恢复

使用飞牛提供的 `TRIM_APPDEST`、`TRIM_PKGETC`、`TRIM_PKGVAR`、`TRIM_DATA_SHARE_PATHS`，不硬编码卷号。两个服务及管理脚本均声明使用专用 `miyabi_native` 包用户，不使用 root 服务，不改系统 Node、systemd 单元或其他应用。

| 位置 | 内容 |
| --- | --- |
| `${TRIM_APPDEST}` | 可替换程序：Go 可执行文件、Node 24、桥接及生产依赖、字体、CA、许可证 |
| `${TRIM_PKGETC}/native-v1.json` | 安装参数与随机密钥；0600，不作为 shell 脚本 source |
| `${TRIM_PKGETC}/mdcz-connection.json` | 提供给 `MIYABI_MDCZ_BOOTSTRAP` 的初始连接；0600 |
| `${TRIM_PKGETC}/mdcz.json` | 桥接基础配置，仅首次复制，后续升级保留 |
| `${TRIM_PKGVAR}/miyabi` | Miyabi 数据库、缓存、介绍与恢复用原图 |
| `${TRIM_PKGVAR}/mdcz` | 桥接配置、图片引用和派生数据 |
| `${TRIM_PKGVAR}/run`、`logs` | 管理锁、进程身份记录、服务日志；日志在再次启动且超过 5 MiB 时轮换一份，尚未做运行期间滚动限额 |
| 飞牛共享目录 `miyabi-native/emby` | NFO、STRM 和导出图片；不包含实际媒体迁移 |
| 飞牛共享目录 `miyabi-native/backups` | 升级/卸载前的完整应用状态备份；不包含外部媒体与导出目录 |

配置值校验后以原子替换写入，密钥不放命令参数或错误信息。更新访问密码会更换 JWT 密钥，使旧登录失效；STRM 令牌不变。MDCz Cookie/翻译密钥继续只由桥接配置模块管理。

升级和卸载前先确认两个进程退出，再复制 `etc`、`miyabi`、`mdcz` 到新的备份目录，最后写 `complete.json`。没有完成标记的目录视为未完成备份；备份失败中止生命周期操作并保持停服，不删除原数据。备份不自动清理，应预留相当于应用状态的一份额外空间，按需手动管理旧备份。备份包含实际凭据，必须限制飞牛共享 ACL；0600/0700 不能替代对飞牛 Windows ACL 的实机核验。

卸载脚本不会删除共享导出目录或备份。具体系统版本对共享目录的卸载处理仍须实机验收；重要备份在卸载前另行复制到独立位置，不能仅凭包脚本承诺整个系统不会清理数据。

恢复时先停止应用，只选带 `complete.json` 的备份；保留当前数据副本，然后恢复上述三个目录，恢复文件所属包用户及 ACL。跨卷或重新安装后核对 `native-v1.json` 中的共享目录路径，重新启动会重建字体配置和初始连接文件。程序和数据必须一起匹配恢复，旧程序未必识别新版本数据库/配置；当前不做无人值守自动降级。

## 生命周期与故障行为

- 启动前检测端口、Node 主版本/架构，实际执行 sharp JPEG 操作，并加载 impit 和 better-sqlite3 原生依赖；依赖错误不进入业务启动流程。字体与 CA 一同随包交付。
- 管理操作由 flock 串行执行，重复启动不增加服务实例。管理器记录 PID、Linux 启动标识和进程开始时间；停止前重新验证，避免信号误发给复用 PID 的无关进程。
- 两边健康接口及协议/引擎校验通过才报告运行；登录保护必须已开启。任一业务进程退出，管理器停止另一进程，应用明确转为停止状态；本版不自动无限重启。用户可在飞牛应用中心重新启动。
- 主服务退出与保存/升级不伪装成功。进程状态写入失败也继续清理两个服务并报告错误。更新访问配置导致启动失败时恢复原安装配置并尝试恢复原服务；若原服务也不能恢复则报告错误，保留配置供排查。
- SIGTERM 先做有界正常退出，超时才结束已确认身份的进程。突然断电或管理器被强杀后，下次操作尝试清理记录内的遗留进程；极短的 spawn 与记录落盘间隙仍需真实系统故障验收，不能宣称完全替代系统级进程托管。

## 构建恢复授权后的准备流程

当前不要运行本节构建/打包命令。源码检查和纯脚本测试不生成 FPK。

1. 在干净的固定 Miyabi 提交上完成 Go、前端及桥接 runtime 回归。Miyabi 原生编译固定 `GOOS=linux GOARCH=amd64 GOAMD64=v1 CGO_ENABLED=0`，先生成内嵌前端；在完整 Git checkout 编译，使 Go 产物保留可核验的 VCS 信息。
2. 按 `extensions/mdcz-bridge` 的固定 MDCz SHA 和 lock 生成 bridge.js 与完整生产 node_modules，保持 pnpm 相对链接。包含 `NOTICE` 和 `MDCz-LICENSE`。原生包不需要 Electron 桌面程序。当前 stage 脚本仍保留上游服务器生产依赖集合，尚未进一步裁剪包体。
3. 准备官方 Linux x64 Node 24 的完整解包目录（含 LICENSE）、NotoSansCJK-Regular.ttc 与字体 LICENSE、CA 根证书。构建者应固定并核对下载来源及校验和；Node 小版本和每项产物哈希写入 bundle.json，不能混用另一 Node ABI 的原生模块。
4. 运行如下准备命令；`--os-min-version` 必须按实际测试目标选择，当前不虚构最低已验证飞牛版本。输出必须是不存在的目录。prepare 会核对 x64 ELF、Node 24、Miyabi GOAMD64=v1/无 CGO/VCS SHA/干净构建标记、桥接 NOTICE、依赖和包内链接。

```sh
python packaging/fnos/prepare.py \
  --miyabi /build/miyabi \
  --bridge /build/bridge-runtime \
  --node /build/node-linux-x64 \
  --fonts /build/noto \
  --certs /build/ca-certificates.crt \
  --version '<待发布版本>' \
  --os-min-version '<实际验证的飞牛版本>' \
  --output /build/miyabi-fpk-stage
```

5. 在输出目录用官方 `fnpack build` 制作候选 FPK。只有完成下列验收才可发布：干净安装、双服务启动、端口冲突、修改密码、MDCz 单片资料/翻译/中文角标、实际库与 Emby 播放、重启 NAS、升级保留数据、备份失败、卸载保留与恢复。J4125 上必须实际加载全部原生模块，并测试中文字体与 TLS，x64 文件头本身不能证明 CPU/GLIBC 兼容。

依赖代码的许可证、Miyabi/MDCz 版本与源码位置保留在包中，公开分发时需同时提供对应源码和构建说明。本轮没有新增发布触发器。

## 从旧 Docker 迁移

初版不自动识别或移动 Docker 数据，安装只创建新应用状态。正式迁移需先停止旧容器并备份其完整数据和环境配置；不能让 Docker 与 FPK 同时打开同一 SQLite 数据库。

按停服状态将 Miyabi 数据复制到 FPK 的 `miyabi` 目录，将已有桥接数据复制到 `mdcz`，并恢复包用户权限。原 Emby 导出目录独立保留并核对新导出位置及 Emby 内部挂载路径。已有保存的 MDCz 服务地址不会被 bootstrap 覆盖，迁移后需明确切换到内置桥接或继续使用原服务。

必须核对旧公开地址与 `MIYABI_STRM_TOKEN`：保留原访问入口和令牌才能继续使用对应旧 STRM。安装文件允许离线填入已有的非空播放令牌；原来没有令牌、原地址无法继续保留、或导出目录变更时，要按实际情况重新导出并验证，不能承诺仅复制数据库就无缝迁移。不要把真实令牌写进仓库、命令参数或问题反馈。

## 检查记录与后续维护

允许执行的检查：

```sh
node --test packaging/fnos/tests/*.test.mjs
PYTHONDONTWRITEBYTECODE=1 python -m unittest discover -s packaging/fnos/tests -p '*_test.py' -v
```

本轮纯 Node 配置/进程管理测试 11 项和 Python 产物格式/图标/包声明测试 3 项通过；另完成 shell 语法、Oxlint/Oxfmt、gofmt 与 diff 检查。进程管理测试启动真实 Node 子进程和 TCP 健康检查，但业务服务/原生库使用模拟实现；本运行器的 PID 与 `/proc` 命名空间不一致，测试临时包使用专门的身份适配器，生产包始终使用 Linux boot ID/start ticks，后者另有单测。在普通 Linux 上测试会自动使用真实 procfs 身份检查。

新增 Go bootstrap 测试未执行；先前 Go/Vitest、真实 MDCz runtime 验证也仍未执行。没有运行 prepare 完整准备、fnpack、Go/前端编译、镜像构建或 NAS 安装。不得将本轮脚本测试记为“FPK 已可用”。

官方规范核对：2026-10-23 读取 [原生应用](https://developer.fnnas.com/docs/examples/native)、[框架与生命周期](https://developer.fnnas.com/docs/core-concepts/framework)、[Manifest](https://developer.fnnas.com/docs/core-concepts/manifest)、[权限](https://developer.fnnas.com/docs/core-concepts/privilege)、[资源](https://developer.fnnas.com/docs/core-concepts/resource)、[入口](https://developer.fnnas.com/docs/core-concepts/app-entry)、[向导](https://developer.fnnas.com/docs/core-concepts/wizard)。后续升级先复核这些契约，再分别提升业务组件和 Node/原生依赖版本。
