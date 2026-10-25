# Miyabi

配合 115 网盘的一站式 Jav & Emby 管理平台。

Miyabi 支持直接对接 115 进行刮削、订阅，也可由 Miyabi 作为底层服务，对 Emby 提供支持。

本仓库是 [Surc/miyabi](https://github.com/Surc/miyabi) 增强版，基于上游 [ppxb/miyabi](https://github.com/ppxb/miyabi)，保留上游来源及许可证。增强功能见[详情扩展](docs/movie-detail-extensions.md)与[作品列表缓存](docs/discover-preload-cache.md)。下方部署示例均使用 Surc 的镜像。

## 项目预览

![媒体库](./screenshots/ScreenShot_2026-09-19_131853_357.png)
![发现](./screenshots/ScreenShot_2026-09-19_132213_340.png)
![搜索](./screenshots/ScreenShot_2026-09-19_132227_063.png)
![设置](./screenshots/ScreenShot_2026-09-19_132249_331.png)

## Docker 部署（Surc 增强版）

> \[!TIP]
> 您可以借助 AI 完成项目的部署，如果要对公网访问，请注意网络安全。

推荐固定版本，避免 `latest` 随后续发布变化：

```text
ghcr.io/surc/miyabi:v0.4.2
```

[v0.4.2 发布说明](https://github.com/Surc/miyabi/releases/tag/v0.4.2)对应的镜像已构建推送。飞牛 NAS 的 Intel J4125 使用 `linux/amd64`，下列示例固定该平台；无需选 arm64。`ghcr.io/ppxb/miyabi` 属于上游项目，不包含本仓库的增强功能。

v0.4.2 已发布 JavLibrary 请求诊断功能；[PR #11](https://github.com/Surc/miyabi/pull/11) 的评论分页及异常响应校验修复已合并至 master，但尚未发布，不包含在本页固定的 v0.4.2 镜像中。该版本的诊断与验证记录见 [v0.4.2 发布说明](https://github.com/Surc/miyabi/blob/v0.4.2/docs/releases/v0.4.2.md)；后续修复状态见[诊断维护文档](docs/javlibrary-diagnostics.md#后续响应校验修复2026-10-24未发布)。

构建发布成功仅证明发布流程完成，不代表飞牛 NAS 实机、Emby 播放或第三方来源已验收。文档修改不改变已有镜像或版本标签；当前发布工作流由推送版本标签或手动运行触发，普通分支提交和合并不会自动发布。

### 1. 首次安装：Docker CLI

**已有容器请直接看后面的升级步骤，不要重新选择数据目录。** 首次安装时先在飞牛文件管理器创建数据目录，复制其真实绝对路径，替换 `/path/to/data`；飞牛的存储卷和用户目录因设备而异，不能照抄其他 NAS 的路径。将密码和宿主机 IP 替换为实际值。命令在 NAS 上具有 Docker 和文件读写权限的管理员终端执行。

这里使用 `--mount`，路径不存在会报错，避免路径写错时自动创建空目录：

```bash
docker run -d \
  --platform linux/amd64 \
  --name miyabi \
  --restart unless-stopped \
  --security-opt no-new-privileges:true \
  -p 8080:8080 \
  --mount 'type=bind,src=/path/to/data,dst=/app/data' \
  -e MIYABI_ACCESS_PASSWORD='change-this-password' \
  -e MIYABI_PUBLIC_URL='http://<宿主机IP>:8080' \
  ghcr.io/surc/miyabi:v0.4.2
```

> [!NOTE]
> 如果配合 Emby 使用，生成的 STRM 播放直链与元数据默认输出在 `/app/data/emby`（对应宿主机目录 `/path/to/data/emby`）。您可将该目录挂载至 Emby 容器作为媒体库。

### 2. 首次安装：Docker Compose（推荐）

也可在飞牛 Docker 的 Compose 项目中使用下面的配置。先创建数据目录，再替换路径、密码和 IP；使用支持长格式挂载的 Docker Compose V2。创建 `docker-compose.yml` 文件：

```yaml
services:
  miyabi:
    image: ghcr.io/surc/miyabi:v0.4.2
    platform: linux/amd64
    container_name: miyabi
    restart: unless-stopped
    security_opt:
      - no-new-privileges:true
    ports:
      - "8080:8080"
    volumes:
      - type: bind
        source: /path/to/data
        target: /app/data
        bind:
          create_host_path: false
    environment:
      - MIYABI_ACCESS_PASSWORD=change-this-password
      - MIYABI_PUBLIC_URL=http://<宿主机IP>:8080
      # 可选：直接通过环境变量预设 Emby 集成（也可启动后在 Web 设置页中配置）
      # - MIYABI_EMBY_ENABLED=true
      # - MIYABI_EMBY_SERVER_URL=http://<Emby_IP>:8096
      # - MIYABI_EMBY_API_KEY=your-emby-api-key
      # - MIYABI_EMBY_MEDIA_PATH=/media
```

执行启动：

```bash
docker compose config -q && docker compose up -d
```

启动后访问 `http://<服务器IP>:8080`。

查看日志：

```bash
docker logs -f miyabi
```

### 原容器升级前：核对与备份

下文假设容器名和 Compose 服务名均为 `miyabi`，容器内数据目录为 `/app/data`。若已有配置不同，请使用原名称和原 `MIYABI_DATA_DIR`，同步调整备份、恢复命令中的容器路径。

先确认实际挂载，`Source -> Destination` 才是正在使用的数据位置：

```bash
docker inspect --format '{{range .Mounts}}{{printf "%s  %s -> %s\n" .Type .Source .Destination}}{{end}}' miyabi
```

| 必须保留 | 核对内容 |
| --- | --- |
| 数据挂载 | `/app/data` 对应的原宿主机绝对路径或原 Docker 卷。不要换成新的 `./data`，也不要更换 Compose 项目名导致卷名变化。 |
| 数据与设置 | 整个数据目录，包括 `miyabi.db`、可能存在的 `miyabi.db-wal` / `miyabi.db-shm`、图片及增强版缓存。115 授权、目录、订阅、代理和 Web 保存的 Emby 配置依赖原数据库；授权本身仍可能过期。 |
| 启动参数 | 原端口、网络、全部环境变量和额外挂载，尤其是 `MIYABI_ACCESS_PASSWORD`、`MIYABI_PUBLIC_URL`、`MIYABI_STRM_TOKEN`；如设置过 `MIYABI_JWT_SECRET`，也一并保留。 |
| Emby | 原导出目录、Emby 容器的媒体挂载路径及 API 配置。如果 `MIYABI_EMBY_DIR` 或 Web 设置中的本地目录位于 `/app/data` 之外，必须另行备份该目录并保留其挂载。 |

SQLite 使用 WAL，先停止 Miyabi 再完整复制，不能运行中只拷贝一个数据库文件。下面的 `docker cp -a` 可从停止的容器复制，适用于 bind 和 Docker 卷；保留文件所有权需要相应权限。如果 Emby 或其他程序也会写入导出目录，备份和恢复期间需暂停这些写入。

先把 `MIYABI_BACKUP` 改为**数据目录之外、已有备份父目录下的全新绝对路径**，确认空间足够。以下命令在同一个终端依次执行，括号内任一步失败就停止；只有全部成功才继续升级：

```bash
MIYABI_BACKUP='/path/to/backups/miyabi-before-v042'
(
  set -eu
  umask 077
  mkdir -m 700 "$MIYABI_BACKUP"
  docker inspect miyabi > "$MIYABI_BACKUP/container.json"
  docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' miyabi > "$MIYABI_BACKUP/container.env"
  docker image tag "$(docker inspect --format '{{.Image}}' miyabi)" miyabi-local:before-v042
  docker stop -t 60 miyabi
  docker cp -a miyabi:/app/data "$MIYABI_BACKUP/data"
  test -s "$MIYABI_BACKUP/data/miyabi.db"
)
```

备份成功后容器保持停止。`miyabi-local:before-v042` 取自备份时实际运行的旧镜像 ID；备份目录、标签与容器名称中的 `v042` 仅表示本次升级目标。若尚未升级且备份失败，可用 `docker start miyabi` 恢复原服务，解决问题后重新备份。配置文件含密码和令牌，应私下保存；Compose 用户还需备份原 Compose 文件、`.env` 及引用的配置文件。额外的 Emby 导出目录也应在停止期间复制。不要在验收前清理本地回滚镜像；重复升级时为备份目录、回滚标签和保留容器选择新的名称。

### 升级到 v0.4.2（选择原部署方式）

**原来用 Compose：** 在原飞牛 Compose 项目或原文件中，只将 `image` 改为 `ghcr.io/surc/miyabi:v0.4.2`，J4125 设置 `platform: linux/amd64`。保留其余配置，不用首次安装模板覆盖原文件。在原项目目录执行；原来指定过 `-f` / `-p` 的，继续使用相同参数：

```bash
docker compose config -q && \
  docker compose pull miyabi && \
  docker compose up -d --no-deps miyabi
```

不需要 `docker compose down`，不要执行 `down -v` 或删除数据卷。

**原来用 Docker CLI：** 下面适用于本文默认的 bridge 网络、`8080:8080` 端口和运行参数。它保留停止的旧容器，用 `--volumes-from` 复用原挂载、用备份的 `container.env` 复用环境变量。如果原来有自定义网络、端口、用户、启动命令等，应沿用原 `docker run` 参数，仅替换镜像并指定 amd64；不要直接套用默认端口。

```bash
(
  set -eu
  test -s "$MIYABI_BACKUP/data/miyabi.db"
  test -s "$MIYABI_BACKUP/container.env"
  test "$(docker inspect --format '{{.State.Running}}' miyabi)" = false
  docker pull --platform linux/amd64 ghcr.io/surc/miyabi:v0.4.2
  docker rename miyabi miyabi-before-v042
  docker run -d \
    --platform linux/amd64 \
    --name miyabi \
    --restart unless-stopped \
    --security-opt no-new-privileges:true \
    -p 8080:8080 \
    --volumes-from miyabi-before-v042 \
    --env-file "$MIYABI_BACKUP/container.env" \
    ghcr.io/surc/miyabi:v0.4.2
)
```

旧容器应一直保持停止，不能让两个 Miyabi 同时写同一份数据。启动后检查 `docker logs --tail 100 miyabi`、原账号/目录/订阅是否仍在，以及作品列表增强功能。若出现全新空库，先停容器核对挂载，不要重新初始化账号或导入数据。

### Emby 保留与检查

- 默认导出位置为 `/app/data/emby`；例如宿主机 `/path/to/data/emby` 挂到 Emby 容器的 `/media`，则 Emby 媒体库和 Miyabi 的 `MIYABI_EMBY_MEDIA_PATH` 都使用 `/media`。已有部署保持原映射，不必改成这个示例路径。
- Emby 的 Web 设置保存在数据库中；启动时优先读取已保存值，环境变量主要用于初始化（本地目录与公开地址为空时才补默认值）。已有配置要调整时，应在「设置」→「Emby」修改，而不能假定改环境变量就覆盖数据库。
- `MIYABI_PUBLIC_URL` 是生成 STRM 使用的 Miyabi 地址，必须可被实际读取该链接的 Emby/播放器访问，不能填写 Miyabi 容器内的 `localhost`。保留原公开地址、映射端口、STRM Token 和 Web 中保存的公开地址；程序启动时会重写 STRM，改错地址会影响现有播放。
- 升级后先做 Emby 连通性测试，再实际播放一部已有影片；仅能打开 Web 页面、容器处于 Running 或 CI 成功，都不能替代播放验收。

### 回滚：旧镜像与升级前数据一起恢复

若新容器已启动过，不能假设旧版本兼容它写入的数据库；回滚会丢弃备份之后的本地变更。先停止新容器，将升级后的整份数据另存，再恢复备份。下面恢复命令**仅用于 `/app/data` 直接绑定一个普通宿主机目录，且没有嵌套挂载的情况**；从当前容器自动读取原路径，避免手填到另一个目录：

```bash
(
  set -eu
  test -s "$MIYABI_BACKUP/data/miyabi.db"
  docker stop -t 60 miyabi
  MIYABI_DATA="$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/app/data"}}{{if eq .Type "bind"}}{{.Source}}{{end}}{{end}}{{end}}' miyabi)"
  test -n "$MIYABI_DATA"
  test "$MIYABI_DATA" != /
  test -d "$MIYABI_DATA"
  test ! -L "$MIYABI_DATA"
  test ! -e "${MIYABI_DATA}.after-v042"
  mv -- "$MIYABI_DATA" "${MIYABI_DATA}.after-v042"
  cp -a -- "$MIYABI_BACKUP/data" "$MIYABI_DATA"
)
```

任一步失败都先解决，不要启动旧版本。如果使用 Docker 卷，以上命令会因不是 bind 而停止：应按 [Docker 卷恢复方法](https://docs.docker.com/engine/storage/volumes/#back-up-restore-or-migrate-data-volumes)，在所有写入者停止时另存卷内现状，再把备份的 `data/` 内容完整恢复到**原卷**。不要删除卷、创建新卷或将新旧数据库/WAL 文件混合覆盖。自定义的外部 Emby 导出目录也要恢复对应备份，保留权限与原挂载。

数据恢复成功后，按原部署方式启动旧版本：

- **Compose：** 恢复原 Compose、`.env` 等配置，把 Miyabi 的 `image` 改为已保存的本地标签 `miyabi-local:before-v042`，在原项目执行 `docker compose up -d --no-deps --pull never miyabi`。不要再次拉取可能已经变化的旧 `latest`。
- **上述 CLI 升级：** 执行下列命令，保留失败容器便于排查，原容器的环境变量、网络和端口随之恢复。若自定义重建时没有保留旧容器，则使用原启动参数与 `miyabi-local:before-v042` 重建。

```bash
docker rename miyabi miyabi-failed-v042 && \
  docker rename miyabi-before-v042 miyabi && \
  docker start miyabi
```

如果升级在新容器创建前就失败、只留下了 `miyabi-before-v042`，数据尚未被新版本打开，可直接将它改回 `miyabi` 并启动。恢复后再次核对账号、媒体库及 Emby 播放；本地数据回滚不会撤销已发给 115 或 Emby 的外部操作。

## 启动配置

启动配置统一使用环境变量，未设置时采用默认值。监听地址与数据目录显式设为空时会报错。

通过 `MIYABI_ACCESS_PASSWORD` 配置访问密码。未配置密码或将密码设为空时关闭门禁。**强烈建议您开启门禁**。

| 环境变量                 | 用途                                                  | 默认值                                        |
| ------------------------ | ----------------------------------------------------- | --------------------------------------------- |
| `MIYABI_ACCESS_PASSWORD` | Web 入口访问密码                                      | 空，关闭门禁（强烈建议配置）                  |
| `MIYABI_PUBLIC_URL`      | 外部访问 Miyabi 的根地址，用于生成 STRM 播放直链      | 默认根据监听端口推导                          |
| `MIYABI_LISTEN`          | HTTP 监听地址                                         | `:8080`                                       |
| `MIYABI_DATA_DIR`        | SQLite 与图片缓存目录                                 | 容器内为 `/app/data`，二进制为 `./data`       |
| `MIYABI_EMBY_DIR`        | STRM 与媒体导出目录                                   | `$MIYABI_DATA_DIR/emby`                       |
| `MIYABI_EMBY_ENABLED`    | 是否启用 Emby 集成通知刷新                            | 配置了服务器或密钥时自动为 `true`             |
| `MIYABI_EMBY_SERVER_URL` | Emby 服务器访问地址（如 `http://192.168.1.100:8096`） | 空，可在 Web 界面动态配置                     |
| `MIYABI_EMBY_API_KEY`    | Emby API 密钥（在 Emby「高级」→「API 密钥」中生成）   | 空，可在 Web 界面动态配置                     |
| `MIYABI_EMBY_MEDIA_PATH` | Emby 容器内挂载的媒体库路径（如 `/media`）            | 空（默认使用本地路径），可在 Web 界面动态配置 |
| `MIYABI_STRM_TOKEN`      | STRM 播放直链访问鉴权 Token                           | 空，未启用 Token 鉴权                         |

> [!TIP]
> Emby 相关配置（服务器地址、API Key、媒体库路径等）除通过环境变量在容器初始化时配置外，也可以在服务启动后随时通过 Web 界面「设置」→「Emby」中进行可视化配置与连通性测试。

## NSFW 警告

本软件可能存在裸露、暴力、色情或冒犯等不适宜公众场合的内容，请勿在公共场合使用本软件，避免不必要的纷争。

## 特别鸣谢

感谢 [莫愁](https://github.com/zk020106) 的 token，感谢 [老罗](https://github.com/luoqiz) 的115账号。

## 致谢

本项目参考了以下项目的部分实现，在此表示衷心的感谢！

- [javdb-cli](https://github.com/FlanChanXwO/javdb-cli)

同时感谢社区 [LinuxDO](https://linux.do) 的帮助。

## 联系我们

[Telegram](https://t.me/miyabi_cn)

## 免责声明

本项目仅供学习、研究和技术交流使用。项目作者与任何第三方服务、原始应用或内容提供方无关。
使用者应自行遵守当地法律法规以及相关服务条款。因使用本项目产生的任何法律、版权、账号、数据或财务风险均由使用者自行承担。

## License

遵循 [GNU GPL v3](./LICENSE) 协议。
