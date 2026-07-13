# 视频画布本地浏览器 E2E Mock

该环境只使用 Node.js 内置模块与项目现有的 Vite，启动时调用本机 `ffmpeg` 在系统临时目录生成 2 秒 H.264/AAC MP4；退出后自动删除临时文件。

## 启动浏览器验收环境

在 `web` 目录运行：

```bash
npm run e2e:video-canvas
```

打开终端输出的登录地址，使用：

```text
邮箱：demo@lingjing.test
密码：video-canvas
```

默认地址：

```text
前端：http://127.0.0.1:4173
Mock API：http://127.0.0.1:18080
视频画布：http://127.0.0.1:4173/login?redirect=%2Fpersonal%2Fvideo-workflows
```

可通过 `VIDEO_CANVAS_WEB_PORT`、`VIDEO_CANVAS_MOCK_PORT`、`VIDEO_CANVAS_WEB_HOST`、`VIDEO_CANVAS_MOCK_HOST` 改写端口或监听地址；`Ctrl+C` 会同时停止 Vite 与 Mock API。

## 自动契约自测

```bash
npm run e2e:video-canvas:contract
```

自测覆盖真实登录与 `/api/me`、19 节点工作流、S02 图片 V2、`video_2` 需更新、四片段 55.8 秒、上传、图片变换、版本签名、修订冲突、10 次幂等启动、运行轮询成功、取消、MP4 Range/If-Range，以及 ffprobe 校验 H.264/AAC 48kHz 双声道。

## 浏览器验收基线

- 1440×900：节点库、全高画布、节点详情三栏完整，默认选中 `S02 图片`，右侧展示 V1/V2、重新生成、上传替换、裁剪、旋转、翻转、提示词与模型。
- 节点详情：所有节点统一包含“上游、状态、历史、输出、参数”五个标签；历史按修订展示，结构化分镜输出包含图片与视频提示词，旧修订输出不会冒充当前结果。
- 画布：`S01 视频` 已就绪，`S02 视频` 显示“需更新”，顺序时间线与最终成片作为固定执行节点显示在画布中。
- 合成配置：四个片段仍保存在顺序时间线节点中，第二段为 1200–12000ms，总时长为 `55.8秒`；页面不再提供独立的底部剪辑面板。
- 小于 1280px：直接显示画布；375px 下隐藏两侧面板，保留完整画布操作区且无页面横向滚动。
