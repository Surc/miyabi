# 可选 Javinizer 评分服务

## 选择与边界

本扩展继续基于 Miyabi，通过独立 Javinizer Go 服务读取 JavLibrary 原始 10 分制评分。默认关闭；没有配置时仍使用原来的 `internal/javlibrary`。对 Amane 的实际爬虫回放未证明换用它能减少本次接入工作，具体对比、固定版本和验证限制见 [验证记录](validation-javlibrary-2026-10-28.md)。

评分接入同时服务既有详情和发现列表；不改变旧 API、响应字段、JavDB/FANZA 分制、磁力排序、文件刮削或下载选源。JavLibrary 评论仍走原客户端，独立请求、分页和重试。Javinizer 没有替代评论接口，评分成功不能证明评论恢复。

PR #13 已合入，v0.4.5 的 Miyabi 程序包含此适配器；原生 FPK 不捆绑或管理独立 Javinizer 引擎。真实站点仍未读到正常页面，扩展维持默认关闭。唯一线上验收清单继续维护在 [诊断文档 A1–A5](javlibrary-diagnostics.md#真实来源与-nas-验收)。

## 模块、接入点和撤销

| 位置 | 职责 |
| --- | --- |
| `internal/javinizer/` | 启动配置、鉴权 HTTP 客户端及响应校验；不包含站点 HTML 解析器 |
| `internal/catalogue/rating_extension.go` | 新的可选 `ExternalRatingReader` 和独立评分缓存 |
| `internal/app/javinizer_extension.go` | 读取启动配置、构造可选依赖；关闭时返回真正的 nil 接口 |
| `extensions/javinizer/` | 固定版本构建、两处上游补丁、配置示例与真实引擎回放工具 |
| `internal/app/app.go`（原文件） | 在数据库等初始化之前检查配置；转移客户端关闭职责；装配新构造函数 |
| `internal/catalogue/movie_extras.go`（已有扩展文件） | 可选评分分支及关闭；原构造函数和评论逻辑保留 |

只修改后两处已有生产文件。旧 `NewMovieExtras(service)` 仍可用；没有修改 CatalogueManager、路由、数据库结构、全局网络、前端或依赖锁文件。

运行时回退：取消 `MIYABI_JAVINIZER_CONFIG` 或把连接配置的 `enabled` 改为 false，再重启 Miyabi。配置只在启动时读取，不需要数据迁移。已保存的列表评分仍按原评分时效到期，必要时使用现有手动评分缓存清理；不自动删除用户缓存。源码撤销：恢复上述两个挂载文件，删除新增模块、测试和工具即可。

## 请求和缓存契约

- 仅调用 `POST /api/v1/scrape`，固定 `force=true`、`selected_scrapers=["javlibrary"]`，使用 Bearer token。不会调用文件整理或媒体下载接口。
- 必须收到 `X-Miyabi-JavLibrary-Identity: page-v1`，并且 `cached=false`、`sources_used=1`、来源为 javlibrary、实际 `movie.code` 和 `movie.id` 均与完整标准化番号一致、无来源错误或评分警告。评分必须大于 0 且不大于 10。
- 上游 404、零分、缺评分不能可靠区分“未收录”“未评分”和网络/解析失败，因此一律保留为来源异常，允许重试，不产生负缓存或伪造评分。当前扩展不提供可靠的合法空评分语义，这是接入限制。
- 没有失败时切回另一来源的隐式降级。显式启用后，连接错误会显示已有的来源异常；配置无效则启动失败，不悄悄换回默认客户端。
- 评分内存缓存 256 项 / 10 分钟，与评论缓存分离。复用 `responseCache` 的共享请求与取消行为：单个调用者离开不取消其他等待者，最后一个等待者离开才取消，失败不缓存。发现列表原有的持久评分时效保持不变（默认 12 小时）。
- 单个 Miyabi 客户端同时最多一个 HTTP 请求；排队也计入总超时，默认 45 秒，可配置 5–120 秒。最多读取 2 MiB，拒绝所有 HTTP 重定向，不向其他目标转发令牌。
- Miyabi 到引擎的连接明确直连，不使用应用内代理或 `HTTP_PROXY/HTTPS_PROXY`。引擎到站点的代理由引擎自身配置；评论仍使用 Miyabi 已保存的代理。

错误正文不复制到日志或浏览器。HTTP 失败仅保留状态码，网络错误使用固定文字并通过 Unwrap 保留原因；不要把任意上游错误字符串拼回公共错误。现有 `extrasError` 继续负责对外错误分类。

## 固定引擎和补丁

使用 [Javinizer Go v1.6.5](https://github.com/javinizer/javinizer-go/tree/fb950d76d7e2eb9416737fa2e9131f91b39c8beb)，固定提交 `fb950d76d7e2eb9416737fa2e9131f91b39c8beb`。上游为 MIT 许可；以后分发引擎时保留其 LICENSE 和版权声明。本仓库只保存小补丁，不复制完整引擎或引入其 Go 模块。

`identity.patch` 仅修改两个上游文件：

1. `internal/scraper/javlibrary/javlibrary.go`：详情必须有真实页面番号，禁止缺失时回填请求番号；增加 `#video_review .score` 旧版括号评分兼容；评分模式在媒体解析及 DMM 图片尺寸探测前返回。
2. `internal/api/movie/scrape.go`：评分模式跳过海报生成；仅在强制、单一 JavLibrary 请求下返回能力标记。

引擎必须带 `MIYABI_JAVLIBRARY_RATING_ONLY=1` 启动。仅关闭 YAML 的 download 字段不足以阻止上游自动探测封面/生成海报。未经补丁的官方 v1.6.5 不会通过客户端校验；不能手工在反向代理伪造能力标记。

`build-engine.sh` 在临时克隆内固定提交、检查并应用补丁，不改传入的源码目录。只支持 Linux amd64、Go 1.27.3、CGO SQLite，设置 `GOAMD64=v1`；二进制版本为 `v1.6.5-miyabi-rating.1`。它构建 API 服务，源码中的 Web 页面是 placeholder，不提供完整 Javinizer 管理界面。当前 Linux 构建通过不等于 J4125/fnOS 的 libc/ABI 已验收。

## 启动与连接

以下供独立测试部署使用，先准备 Go 1.27.3、Git、C 编译器和 Python 3。路径示例可按实际服务账号调整；引擎工作目录与 Miyabi、MDCz 以及真实媒体目录分开。

在 Miyabi 仓库根目录构建并准备配置：

```sh
rating_dir="$HOME/miyabi-rating"
install -d -m 700 "$rating_dir/data"
extensions/javinizer/build-engine.sh "$rating_dir/javinizer"
cp extensions/javinizer/engine.example.yaml "$rating_dir/engine.yaml"
chmod 600 "$rating_dir/engine.yaml"
cd "$rating_dir"
MIYABI_JAVLIBRARY_RATING_ONLY=1 ./javinizer --config engine.yaml web
```

可以用 `GO_BIN=/绝对路径/go` 选择工具链，或给构建脚本第二个参数指定已有引擎 checkout。示例监听 `127.0.0.1:8765`，无外网暴露、无媒体挂载，版本检查及图片/翻译下载关闭。保持每次启动的工作目录一致，SQLite 与认证资料存放在该服务目录。

首次启动时，在同一主机另一个终端初始化独立账号；密码只交互输入，不进入命令参数或源码：

```sh
python3 - <<'PY'
import getpass, json, urllib.request
body = {"username": "miyabi", "password": getpass.getpass("新引擎密码（至少 12 字符）: ")}
request = urllib.request.Request(
    "http://127.0.0.1:8765/api/v1/auth/setup",
    data=json.dumps(body).encode(), headers={"Content-Type": "application/json"},
)
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
with opener.open(request, timeout=10) as response:
    print("初始化状态:", response.status)
PY
```

初始化仅执行一次。停止引擎进程，在同一工作目录创建专用令牌和连接配置，再按前述命令启动引擎：

```sh
umask 077
./javinizer --config engine.yaml token create --name miyabi-rating --json > token.json
python3 - <<'PY'
import json
from pathlib import Path
token = json.loads(Path("token.json").read_text())["token"]
connection = {"enabled": False, "server_url": "http://127.0.0.1:8765", "api_token": token, "timeout_seconds": 45}
with open("connection.json", "x") as output:
    json.dump(connection, output, indent=2)
Path("connection.json").chmod(0o600)
Path("token.json").unlink()
PY
```

引擎示例把日志写到 stderr，避免污染 CLI 的 JSON 输出。已存在 connection.json 时脚本拒绝覆盖。令牌轮换使用上游 token create/revoke 命令，令牌不回传 Miyabi 设置页。实际服务账号必须能读取连接文件；不要提交或分享该文件。

验证环境准备好后，把连接文件 `enabled` 改为 true，在 Miyabi 进程设置 `MIYABI_JAVINIZER_CONFIG=/绝对路径/connection.json` 并重启。`server_url` 只接受 HTTP(S) origin，不含账号、路径、query 或 fragment。跨主机部署需改为受控的服务地址；127.0.0.1 在不同容器中并不共享。

引擎需要站点代理时，在 engine.yaml 的 `scrapers` 下配置上游已有的 proxy profiles，重启引擎；不修改 Miyabi 的全局网络配置：

```yaml
scrapers:
  proxy:
    enabled: true
    default_profile: site
    profiles:
      site:
        url: http://127.0.0.1:7890
  javlibrary:
    proxy:
      enabled: true
      profile: site
```

上面是合并到既有节点的片段，不重复创建两个 scrapers 节点。可选 FlareSolverr 沿用引擎的原配置，本轮没有启用或验证。其上游调用不接收请求 context，HTTP 超时后浏览器任务可能继续；一个 Miyabi HTTP 槽位不代表全局只有一个浏览器实例。J4125 上需另验内存、排队与取消后回收，再决定启用。

## 升级检查

1. 分别记录 Miyabi、Javinizer 最新 SHA；先升级一方并验证，不同时替换两个运行时。
2. 若上游已修复真实番号、评分选择器或图片副作用，逐项移除对应补丁；能力标记必须仍代表相同契约，不因能编译就保留旧标记。
3. 核对 API 的 code/id/source/cached/errors/rating 字段和认证规则；不从聚合分或其他来源推断 JavLibrary 分数。
4. 运行下述验证记录中的 Go、race 和真实引擎回放；确认默认关闭行为、评论失败隔离、取消和图片请求仍符合要求。
5. 由 NAS 的真实来源验收决定是否启用和发布，分别记录评论、评分、代理路径及资源占用；不要把合成夹具当作线上页面证据。
