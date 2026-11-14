# MDCz 模块化落地记录（未发布）

## 基线与范围

- Fork：`Surc/miyabi@01d694540fb96901affd9c31f9a9471a727fddf2`，本次再次核对远端 `master` 一致。
- 设计：2026-11-14 恢复复核版《Miyabi 与 MDCz 单 FPK 架构评审》。
- 初始连接/隔离按 MDCz 0.19.1 验证；随后隔离验证 MDCz 0.22.0 真实 runtime 通过，来源锁提升为 `e869dc74e44d7e21d5ad5ed33ad9e40e5abed9be`。此次**不打 FPK、不发布、不部署**，不修改 README。
- 本文记录源码实施及自动化验证；不是 NAS 验收或生产就绪声明。

## 本批代码

### 连接、装配与设置

`internal/app/mdcz_extension.go` 显式组装扩展，原生来源设置直接委托原生 metadata 服务，只有图片能力按 provider 进入 MDCz；`internal/mdcz.Service` 不再匿名嵌入整个原生服务。共享依赖检查纳入 `internal/mdcz`，仅允许它向原生适配层单向依赖。

`MIYABI_MDCZ_MANAGED` 指定受管连接文件时，所有测试、网页配置、资料、图片和角标均由 `effective` 解析。数据库旧地址/密钥保留给回退，不作运行时自动回退。文件缺失/无效导致增强功能不可用，不阻止原版设置和库浏览。数据库读取错误仍然阻止核心启动，不能把核心存储错误当可忽略扩展错误。扩展原图目录错误只让扩展不可用。

连接 JSON 包含 `server_url`、`api_key`、非敏感 `generation`（目标地址的 SHA-256）。API 仅回显地址、是否有密钥、模式、generation 和配置状态，不返回密钥。配置状态 `configured` **不是在线健康承诺**；真实健康与版本通过“测试连接”检查。

新安装配置写入 `mdczConnectionMode: "managed"`。已有安装配置无此字段时仍走旧 bootstrap 逻辑，防止升级时擅自切换旧 Docker。显式迁移应在停机、备份及数据副本验证后把该字段改为 `managed`，重启后检查实际单片请求与设置均使用包内实例。这里未对任何 NAS 执行迁移。

配置读取响应含 `connection_generation`，来源配置保存必须回传最初读取的目标身份。缺失或不符返回明确冲突，先重新读取，不把旧草稿送到新桥接。该字段仅由 Miyabi 校验，不转交 MDCz schema；旧客户端需更新/重读后保存。

受管 UI 将连接地址设为只读、密钥禁止编辑。离线时可切回原版；来源/代理表单未保存时仍要求先保存或放弃，不静默丢稿。

### 单次尝试的一致性

`BeginAttempt` 在刮削开始时固定选项、连接、引擎和配置 ID。持久 `attempt_id` 只保存非敏感摘要，不保存密钥。恢复时不匹配返回冲突，旧的已有候选但缺少 attempt ID 的任务也明确要求重新刮削，避免不知情地复用旧候选。

metadata/images/poster 请求带预期配置 ID 和引擎 SHA。bridge 在与配置修改相同的互斥窗口核对；冲突返回 409，Go 上抛为 `KindConflict`，图片候选循环不吞掉它。衍生角标缓存纳入连接 generation 与配置 ID。离线 `ExportLocalMovie` 不受此在线快照钩子影响。

### 故障隔离和版本锁

见 `fnos-native-package.md` 与 `mdcz-compatibility-lock.md`。主程序和 bridge 健康分离；bridge 重启有有限指数退避与预算，stop 中断等待及探针，保留身份校验。一个 source lock 生成 Go、bridge、FPK 的严格兼容组合，未放宽为只校验协议号。

### 候选和后续字幕任务

资料准备阶段只存 Task 候选，不提前更新正式 Movie 与演员/标签索引；最终 publish 同事务写入两种引擎的资料和索引。字幕目标在最终事务内写入现有任务表，以 `subtitle` 类型交由独立任务池执行，替代易丢失的私有内存队列。旧任务载荷可解码，但旧候选未记录配置快照需重新刮削。回退旧程序前必须处理新增 subtitle 待执行记录，不能让不认识该任务类型的程序直接消费。

## 可恢复发布与字幕落盘

`internal/export/journal*.go` 在应用数据目录的私有 publication store 中保留日志、旧/新文件及摘要，不需要导出父目录写权限，随应用数据备份，通过 operation ID 和同一 SQLite 提交中的 Task receipt 判定提交结果。已提交完成新附件，明确未提交恢复旧附件，未知提交或外部修改则阻断写入。持久根目录身份防止挂载消失后悄悄向替代目录写入。仅管理已知生成文件与数据库归属字幕，不复制视频原件。

`internal/subtitle/recovery.go` 将自动字幕任务的下载候选及文件/电影版本身份先保存为 SQLite pending，使用 fsync 临时文件与不覆盖硬链接安装，登记字幕与删除 pending 同事务。未知数据库结果不删除已写文件；重启可离线完成登记。所有 export Manager 写入之前先恢复发布日志及自动字幕 pending，异常只阻断写入，不阻断只读页面。任务页显示恢复阻塞及字幕独立进度。

## 验证及未完成边界

- Go 1.27.4 来自官方 go.dev，校验官方 SHA-256；依赖按现有锁安装，未修改版本。
- React：53 个文件、330 项测试通过，typecheck、lint、格式检查通过。Web Node：18 项通过。
- Bridge/FPK Node：48 项通过；FPK Python：5 项通过；兼容性生成检查通过。
- MDCz 0.22.0：隔离 TypeScript、4 项真实 runtime 回归、测试编译通过。覆盖编号规则、旧演员别名、角标像素、实际 sharp/impit HTTP 与取消。
- 全量 `go test -tags dev -p 2 -timeout 20m ./...` 通过；app/export/library-scrape/subtitle/tasks/mdcz/api 七个关键包 `-race` 全部通过。自动化涵盖 SQL 失败、未知提交、启动恢复、外部变更拒绝、身份变化与字幕落盘中断。

以下边界仍未完成，不能称为全设计生产验收：

1. 外部进程必须停止并发修改受管路径。文件系统不是 CAS 事务服务，摘要校验与替换之间发生的非合作写入不保证保全。
2. 未在真实 NAS 验证断电、卸载挂载、重启、ACL、ABI、字体、资源占用和真实来源站点账号。
3. Miyabi 上游正式历史合并未执行；已知 9 个冲突应分开审核，不在本次盲目合入。
4. 用户要求先不打包，未构建 FPK/Docker 产物、发布或部署。旧 bridge 1.0 与新严格兼容组合不兼容，升级需成套；旧安装不会被自动切换为 managed。
5. 手动字幕下载/上传仍使用既有先 Link 后 SQL 路径，未接入本次 pending 协议；该窗口仍可能出现文件已写而数据库未登记。本次恢复保证仅覆盖自动字幕任务，不代表所有字幕写入均具备事务恢复。
6. 降级前必须停机并备份匹配的数据库、导出附件与私有恢复目录，旧程序不识别新字幕任务/发布凭据，不能直接消费。

## 建议英文提交信息

`Add recoverable MDCz integration and durable subtitle handoffs`

本次仅生成建议，不自动提交。

## Astra 初审修复

- v2 stage 移入应用私有数据 store；旧 v1 外置日志保留并阻断，不自动迁移；存在此类日志时手动备份/卸载备份明确失败，不写 complete，须保留原 stage 先恢复处理。
- FPK 备份写 complete 前验证复制后的恢复目录与 blob 哈希，拒绝缺失、旧外置 stage 和符号链接。
- 发布状态在页面挂载时每 10 秒持续读取，不依赖任务事件才能发现新阻塞。
- 完成回调读取持久 Completed，已提交后日志收尾失败不再降低 Movie 状态。
- 自动字幕恢复对已存在的匹配文件重新同步文件和父目录；同步失败保持 pending。
