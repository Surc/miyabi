# v0.4.0 发布维护

2026-10-23 用户明确“合并发布”，恢复本版必要的生产构建、镜像、FPK、合并、标签和 Release；不包含 NAS 部署。该授权覆盖此前暂停构建要求。功能回归见 [验证记录](validation-2026-10-23.md)。

## 产物和发布顺序

1. 先核对 PR #6，再将 #7 调整到 master 并核对 Git tree；保留提交历史，不混入其他文档草稿。
2. 新的正式构建发现 Vite SSR 不使用 `build.lib.fileName`，导致生成 main.js 而启动入口要求 bridge.js。已在自有 Vite 配置显式指定 Rollup entryFileNames；实际 bridge.js 已启动通过。运行源码级 runtime 测试无法代替此项检查。
3. `packaging/fnos/build-release.sh VERSION OUTPUT` 必须在干净 Git checkout 执行，输出在仓库外；固定 Node 24.19.0、fnpack 1.2.3、Noto Sans CJK Sans2.004 官方来源和 SHA-256，MDCz SHA 从 contract 读取。生产依赖安装在 MDCz workspace 外，防止 pnpm 误剪裁开发 workspace。
4. 构建器保留前端、Go CGO=0/AMD64=v1/VCS 信息、独立桥接、原生依赖、许可证、字体和 CA；prepare 验证身份和包内链接。`smoke.mjs` 使用临时数据、随机测试凭据及回环端口启动真实 Miyabi/桥接，检查鉴权、配置、静态 UI 和 sharp/impit/SQLite，再由官方 fnpack 打包。
5. GitHub 工作流支持 master 上手动输入 `0.4.0` 发布，也保留版本标签触发。先创建草稿 Release，构建 amd64 主镜像、桥接镜像和 FPK；三者成功、附件上传后才公开 Release。已公开版本不覆盖；失败保留草稿，检查日志后只重跑失败任务。

主镜像为 `ghcr.io/surc/miyabi:v0.4.0`，桥接为 `ghcr.io/surc/miyabi-mdcz:v0.4.0`。不再构建 ARM64。原生附件为 `miyabi-native_0.4.0_linux-amd64_preview.fpk`、bundle.json 和 SHA256SUMS。

## 尚未证明的兼容性

原生包使用 Ubuntu 22.04 CI 构建，以降低对运行时 GLIBC 的要求；这不是 J4125/fnOS 验收。候选 manifest 声明 fnOS 1.0.0，真实最低兼容版本尚未确定。此 FPK 明确以预览附件交付，完成安装/升级/卸载、ACL、中文字体/TLS、故障恢复与 Emby 流程后再提升为正式原生包。不要把本轮构建/回环检查写成 NAS 实测，也不自动迁移用户数据。

## 后续升级检查

分别更新 Miyabi 和 MDCz，按扩展文档复核契约。更新 Node、fnpack 或字体时重新核验官方来源、SHA-256 和许可证；Node 主版本改变需重建全部原生依赖。保留启动 smoke，不能只检查产物文件存在。发布时核对镜像架构、revision、Release 标签和附件校验和，保存运行链接与最终 SHA 到维护 skill。
