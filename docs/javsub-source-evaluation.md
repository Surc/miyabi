# JavSub.ai 备用字幕源评估（2026-10-31）

结论：暂作为人工检索备选，未加入默认自动字幕源。当前保留迅雷、SubtitleCat、AVSubtitles。

## 已确认

- 官网提供按番号搜索的入口。[首页](https://javsub.ai/)
- 中文分类页列出 7,187 部作品，明确标注字幕由 AI 生成。[中文目录](https://javsub.ai/subtitles/chinese/)
- 免费区单独列出中文 32 份、繁中 23 份，并声明免费字幕无需账号。两个数字不是去重作品数，也不能用完整目录的数量代替免费可下载覆盖量。[免费区](https://javsub.ai/free/)
- `JUR-786` 的中文条目提供 Demo 和获取完整字幕的入口；页面说明通过邮箱发送订单链接，付款后下载完整 SRT。不能将 Demo 作为完整字幕候选。[条目](https://javsub.ai/subtitles/jur-786-75842/)
- 免费条目 `MIAA-483` 的繁中轨道明确提供 `Download free`，链接为 `/download/free/d002403c-85f7-4591-a0dc-4d66fd62ee7a/`。[条目](https://javsub.ai/subtitles/miaa-483-74174/)

## 自动接入尚缺的验证

本机直接请求免费条目 `https://javsub.ai/subtitles/ipzz-995-82141/`、`JUR-786` 详情，以及上述 `MIAA-483` 免费完整字幕端点，均收到 HTTP 403 Cloudflare 拦截页。免费端点返回 `text/html`，没有字幕时间轴，未能完成中文 SRT 的实际下载。这是站点返回的访问拦截，Fake-IP 修复不能解决此类 HTTP 403。本轮未找到可用的公开 API 文档。

技术上的候选接入范围是明确标注免费的完整中文字幕；当前缺少稳定可访问的下载链路，暂不能把它作为可靠自动备用源。接入前需要先验证公开中文 SRT 下载链路或官方 API 条件。
