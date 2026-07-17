# 视频画布交付需求矩阵

| Requirement ID | Behavior | Affected area | Verification | Status |
| --- | --- | --- | --- | --- |
| REQ-001 | 保存已确认的桌面效果图与可复用提示词 | `docs/ui-prototypes/video-workflow` | PNG 尺寸检查与人工视觉确认 | done |
| REQ-002 | 1440×900 沉浸式桌面工作区，浅色框架、深色画布、五区布局 | Web 画布页面与组件 | 59 项 Vitest、生产构建、1440×900 浏览器验收 | done |
| REQ-003 | 导航、多选、节点复制删除、分组对齐、类型化连线、快捷键、撤销重做 | Graph 工具与画布组件 | Graph 单测、组件测试及浏览器交互验收 | done |
| REQ-004 | 图片生成、上传替换、裁剪旋转翻转、不可变版本切换 | 素材服务、图片检查器 | Go 单测、组件测试、Mock API 契约与浏览器旋转验收 | done |
| REQ-005 | 单轨 1–4 段排序及 100ms 入出点裁剪 | Graph v2、时间线、合成器 | 时间线边界单测与 FFmpeg 55.8 秒验证 | done |
| REQ-006 | Graph v2、通用模板、19 节点古风模板、v1 兼容升级 | 前后端类型、模板与迁移 | Graph/模板测试与 MySQL 8.0 迁移往返验证 | done |
| REQ-007 | 素材列表上传删除、版本签名、Range 读取、用户隔离 | API、DAO、媒体安全 | Handler/DAO/媒体安全测试与 Mock Range 契约 | done |
| REQ-008 | 持久化 DAG 运行时，图片并发 2、视频并发 2、合成并发 1 | 后端 runtime | runtime 全链路、失败注入与竞态测试 | done |
| REQ-009 | 估价、幂等预扣、结算退款、缓存、审批、取消、重启恢复 | 计费与运行状态机 | 10 次并发幂等、缓存、审批、恢复及旧渠道凭据测试 | done |
| REQ-010 | 3 种画幅、720p/1080p、H.264/AAC/30fps、裁剪和误差 ≤50ms | FFmpeg 合成器 | 六组输出媒体探测与四片段时长测试 | done |
| REQ-011 | 新逻辑具备单元、组件、集成、端到端及真实烟测证据 | Go/Vitest/浏览器/真实渠道 | 自动化与浏览器通过；Wan2.7 四幕运行已生成 3 段 1080×1920、15.023 秒视频，第四段因上游额度不足待续跑 | partial |
| REQ-012 | 迁移、备份、readyz、非 root 容器、quick sync 与安全回滚 | 配置与部署 | 远端 quick sync、迁移、readyz、非 root、FFmpeg 与最新一致性快照均已验证 | done |
| REQ-013 | 内置古风预设先产出故事圣经、人物小传、场景设定、分幕剧情、分镜镜头与 Seedance `@图片` 引用契约 | 模板、前期材料资产路线图 | `go test ./internal/videoworkflow`，模板断言覆盖材料包字段、分镜时间段和 Seedance 引用 | done |

完成门槛：所有条目均为 `done`，无未解决 P0/P1 审查问题，相关测试和构建全部通过。
