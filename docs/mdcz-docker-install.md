# 飞牛 MDCz Docker 安装记录

2026-10-31 在 `192.168.1.6` 安装 `miyabi-mdcz-bridge`。这是供 Miyabi 使用的 MDCz 桥接服务，提供资料、配置和封面处理接口。

- 服务地址：`http://192.168.1.6:3841`，映射容器端口 3840。
- 引擎：`f65e7d4161151aff1531f3b99872e49234d5d539`，协议 1、配置能力 1。
- 运行镜像：`node:24-bookworm-slim`，程序取自已安装原生包的相同固定版本桥接快照，不在宿主机安装依赖。
- 持久目录：`/vol1/docker/miyabi-mdcz-bridge`。`program/bridge`、`program/fonts`、`fonts.conf` 和初始 `mdcz.json` 只读挂载；`data` 独立可写。没有挂载媒体库、原生服务数据或 Docker socket。
- 随机桥接密钥保存在容器环境配置中，不写入仓库。安装端另保存 Windows DPAPI 加密副本。
- 内存上限 512 MiB、CPU shares 256，启用自动重启。

程序快照中的 pnpm 链接已修复为快照内部的相对路径，运行不依赖临时复制容器或原生包目录。后续升级仍应遵守 [MDCz 固定版本与协议约定](mdcz-extension.md)，不能只替换引擎或程序文件的一部分。

已验证带密钥的健康请求返回预期版本，未鉴权请求为 401；使用纯色测试图片调用封面角标接口返回 JPEG 和预期资源标签，没有请求真实影片站点。真实站点连通性、资料质量与 NAS 长时间占用未验收。

Miyabi 原生包已从 0.4.8 升级到 0.4.9，并切换到上述 Docker 服务。原有 MDCz 初始配置、持久配置及资源已迁移到 Docker 持久目录；Miyabi 内保留角标开启、左下角、翻译关闭的原设置。

原生安装配置 `native-v1.json` 的可选 `externalMDCz` 对象保存 `serverURL` 和 `apiKey`。启动时据此生成连接文件，检查外部服务健康，只启动 Miyabi 子进程；未配置时沿用内置桥接。已在升级及停止、启动后核对连接仍为 Docker 地址，原生监督器只有一个子进程。切换前配置备份为同目录的 `native-v1.pre-docker.json`，包含凭据，应按私密配置保管。

AVOP-125、IPX-906 已使用 0.4.9 的导出实现和现有资料重新导出，详见 [多版本与分段导出](multi-version-export.md)。部署验证使用临时管理容器，完成后移除；常驻 MDCz 容器不保留原生配置、数据库或媒体目录的挂载。浏览器与真实播放验收由用户完成。

2026-11-01 批量修复后，当前原生包为 0.4.12，Docker MDCz 地址、配置及运行容器保持不变。原有 JavLibrary 浏览器连接配置也已保留。详见 [批量修复记录](validation-bulk-multi-version-2026-11-01.md)。
