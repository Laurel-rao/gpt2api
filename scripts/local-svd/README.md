# 本机视频服务（Seedance 兼容）

协议：`POST/GET /seedance/api/v3/contents/generations/tasks`

## 启动

```bash
bash scripts/local-svd/start.sh
```

默认 `http://127.0.0.1:8792`，密钥 `local-svd-key`，引擎 `motion`（秒级）。

**勿停/杀 `:8791` 的 local-sd（SD-Turbo 生图）**；imagegen 依赖它。无参考图时 motion/svd 会调用 `:8791`，两边应同时存活。

## 引擎

| `LOCAL_VIDEO_ENGINE` | 说明 | 预期耗时 |
|----------------------|------|----------|
| `motion`（默认） | 参考图 Ken Burns + ffmpeg | 约 2–10 秒 |
| `svd` | Stable Video Diffusion XT | M4 约数分钟～更久；且与 SD 争 MPS，日常勿开 |

```bash
# 仅调试真 SVD 时（会占 MPS；日常请用默认 motion）
LOCAL_VIDEO_ENGINE=svd bash scripts/local-svd/start.sh
```

权重：`scripts/local-svd/models/svd-xt/`（约 4.2GB）。

## 接入 gpt2api

```
videogen.channel_type = apiyi_seedance2
videogen.apiyi_seedance2.base_url = http://127.0.0.1:8792
videogen.apiyi_seedance2.api_key = local-svd-key
```

写库后 `POST /api/admin/settings/reload`。
