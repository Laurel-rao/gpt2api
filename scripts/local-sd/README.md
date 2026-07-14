# 本机 SD-Turbo 生图（Apple MPS）

OpenAI 兼容：`POST /v1/images/generations`、`/v1/images/edits`

## 启动

```bash
bash scripts/local-sd/start.sh
```

默认：`http://127.0.0.1:8791`，密钥 `local-sd-key`。

首次需从 hf-mirror 下载权重到 `scripts/local-sd/models/sd-turbo/`（约 2.4GB）。若目录已存在则离线加载。

## 接入 gpt2api

```
imagegen.base_url = http://127.0.0.1:8791/v1
imagegen.api_key  = local-sd-key
imagegen.enabled  = true
```

然后 `POST /api/admin/settings/reload`。

模型对外仍报名为 `gpt-image-2`（兼容现有前端），实际推理为 `stabilityai/sd-turbo`。
视频工作流检查器「执行模型」下拉里显示为「SD-Turbo (本地)」，value 仍为 `gpt-image-2`。
