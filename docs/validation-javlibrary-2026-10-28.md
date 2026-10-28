# JavLibrary 开源方案对比与接入验证（2026-10-28）

## 基线与结论

- Miyabi fork master：`f4073dd9ffbfceb7fb65c067add5e1270f86f678`；ppxb/miyabi：`d3c86548b477ed2d84eb46bef92dd25f64b4e566`。
- [Amane v0.23.0](https://github.com/sqzw-x/amane/tree/4230de458c3197e1e17b6c5b42cc385fc6961d15)：`4230de458c3197e1e17b6c5b42cc385fc6961d15`。比较时 main 为 `c0e1a3504427635d2e515e1c31642b60c45a7985`，JavLibrary 文件 blob 同为 `c7ab645a39081501c9f26fc312936502298ae6a5`。
- [Javinizer Go v1.6.5](https://github.com/javinizer/javinizer-go/tree/fb950d76d7e2eb9416737fa2e9131f91b39c8beb)：`fb950d76d7e2eb9416737fa2e9131f91b39c8beb`。

继续用 Miyabi，采用可选独立 Javinizer 评分服务，并用两文件补丁约束身份、评分兼容和图片副作用。Amane 的源码和运行结果未证明它更适合本次评分接入；不引入另一套 Python 运行时或移植整套项目。设计、配置和撤销方式见 [接入文档](javinizer-rating-extension.md)。

## 实际程序与合成页面对比

使用中性合成 HTML，不包含用户媒体、真实影片正文、登录 Cookie 或站点快照。测试运行原项目的真实解析/请求代码，不是重写解析器后自测。两组契约用例数量和目的不同，不把通过比例当作项目总体质量或线上成功率。

| 场景 | Amane 原爬虫 | Javinizer 官方 v1.6.5 / 本次补丁 |
| --- | --- | --- |
| 旧 `#video_review .score` 括号评分 | 可读取 | 官方缺失；补丁兼容，并拒绝单边括号 |
| `$rating` 及 `#video_rating span.num` | 对应夹具未取到分数 | 可读取 |
| 搜索直接返回详情 | 返回 None | 可读取 |
| `./?v`、相对 `.html` 链接 | 丢失 /cn/，夹具返回 404 | 覆盖旧查询链接和实际 .html 跳转读取 |
| 前缀错配、首项回退、title/href 缺失对齐 | 部分场景返回其他番号 | 实际返回番号由 Miyabi 严格校验，错片拒绝 |
| 详情缺少实际番号 | 返回 None | 官方两种场景回填请求番号；补丁拒绝 |
| 超范围评分 | 99 被保留 | Miyabi 拒绝 0、负值、超 10、缺评分和警告 |
| 无图片读取 | 17 项中 0 图片请求 | 官方 12 次被代理拦下的图片 CONNECT；补丁 0 次 |

Amane：Python 3.14.8、冻结依赖，实际 `JavLibraryCrawler`、parsel、HttpClient、WebClient/curl_cffi；checkout 无改动。17 项接入需求中 8 项满足、9 项缺口，包括上述字段/匹配问题；空响应和挑战页有明确分类。

Javinizer：独立临时数据库、账号和令牌，真实 API 进程经过本地夹具代理请求。官方二进制 14 项中 11 项满足，失败为两项缺身份误接受及旧评分字段缺失。修补后 14 项全部通过，实际请求一次 .html 详情，图片请求为 0。`build-engine.sh` 重新构建的产物也完成同样 14 项回放，不只验证开发目录的二进制。

| 验证对象 | SHA-256 |
| --- | --- |
| 官方 Linux amd64 二进制（已核对上游 checksums） | `c0979f9c651b365001a00a4a109b5c04b2582094ff91645cc9ee873545392887` |
| 本仓库构建脚本生成的二进制 | `193dda3cf48cf76f39dbaf45980496b5829ed7ebeffe21bc7f906c2efdb3c544` |
| identity.patch | `090bf3d49a3f7c4472c4d0b4ee4d3626a45dea69b7a1c9d708fa811c8f1928d5` |

产物摘要记录本次实际测试对象，不承诺跨 C 工具链构建的二进制完全一致。没有发布这些二进制。

## Miyabi 回归

Go 1.27.3；普通回归及 race 覆盖相同六包。表中是包含子测试的 pass 事件数，不是用例函数数量。

| 包 | 普通通过 | race 通过 | 跳过 |
| --- | ---: | ---: | --- |
| internal/javinizer | 45 | 45 | 0 |
| internal/catalogue | 44 | 44 | 0 |
| internal/app | 44 | 44 | 1，既有外部真实人像夹具缺失 |
| internal/javlibrary | 98 | 98 | 0 |
| internal/netx | 51 | 51 | 0 |
| internal/api | 293 | 293 | 0 |
| 合计 | 575 | 575 | 每轮 1 |

新增回归验证默认行为、真正 nil 接口、无效配置/额外字段/null、鉴权请求、能力标记、实际完整番号、原始分制、404/缺分/警告不伪装为空成功、大小限制、重定向不转发令牌、排队和读取正文期间取消、共享请求取消、失败重试、评分与评论缓存隔离、两客户端关闭。

Node 独立逻辑 14 项通过；gofmt、Python 编译、shell 语法、差异空白检查通过。前端无改动，本次没有重跑 Vitest、浏览器或前端生产构建。`-tags dev` 用于不依赖 web/dist 的 Go 测试，不代表 Miyabi 生产构建或 FPK 打包通过；未运行全量 codeid 大语料。

实际配置验证：原样使用 engine.example.yaml，仅测试端口换为空闲端口；启动、账号初始化、CLI JSON 令牌创建、停止再启动的认证持久化通过。无令牌请求为 401，有令牌但缺 scrape 参数为 400，未发起真实站点请求。首次发现 stdout 日志污染 token create --json，已将示例日志改为 stderr 后重测通过。

## 复现命令

在 Miyabi 根目录，使用 Go 1.27.3 和现有锁定依赖：

```sh
go test -tags dev ./internal/javinizer ./internal/javlibrary ./internal/netx ./internal/catalogue ./internal/api ./internal/app
go test -tags dev -race ./internal/javinizer ./internal/javlibrary ./internal/netx ./internal/catalogue ./internal/api ./internal/app
node --test web/tests/*.node.mjs
bash -n extensions/javinizer/build-engine.sh
python3 -m py_compile extensions/javinizer/test_engine.py extensions/javinizer/compare_amane.py
extensions/javinizer/build-engine.sh /tmp/javinizer-rating
python3 extensions/javinizer/test_engine.py /tmp/javinizer-rating --require-identity-header
git diff --check
```

回放工具仅用 Python 标准库，默认离线、临时状态、不触碰媒体目录；返回 0 才表示契约通过。官方二进制可直接作为参数运行，当前预期返回 1，表示检测到了基线缺陷，不将它修改成“通过”。

Amane 对比工具要求固定版本且 tracked 工作树干净；使用其 uv.lock 安装 Python 3.14 和冻结 core 依赖后运行：

```sh
PYTHONPATH=/path/to/amane/src /path/to/amane/.venv/bin/python \
  extensions/javinizer/compare_amane.py --source /path/to/amane --output /tmp/amane-result.json
```

当前预期返回 1，表示 9 项需求缺口。`--live` 才请求真实站点；Javinizer 的对应可选参数为 `--live IPX-535`，真实探测仍使用临时状态，不自动启用生产配置。

## 真实来源与剩余限制

本轮 Amane 的 IPX-535 直连实际请求失败，原始诊断为 curl DNS 错误；Javinizer 官方 API 真实探测为 HTTP 404，没有电影结果，不能区分未收录与请求/解析失败。没有任何一次获得正常线上详情页，因此不能说 JavLibrary 数据已经恢复，也不能把运行器现象当作用户 NAS 的根因。

本次只完成代码、离线/本地集成验证和 PR。J4125/fnOS、真实来源、浏览器、评论分页、可选 FlareSolverr、NAS 代理和内存占用均未验收。保留默认关闭；合并、发布、FPK 打包与 NAS 部署不在本次执行范围。线上验收项仍见 [诊断文档 A1–A5](javlibrary-diagnostics.md#真实来源与-nas-验收)，不另造已完成的线上清单。
