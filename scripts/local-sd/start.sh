#!/usr/bin/env bash
# 启动本机 SD-Turbo 生图服务（OpenAI 兼容 /v1/images/*）
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"
if [[ ! -d .venv ]]; then
  uv venv --python 3.11 .venv
  # shellcheck disable=SC1091
  source .venv/bin/activate
  uv pip install torch torchvision
  uv pip install diffusers transformers accelerate safetensors fastapi uvicorn pillow
else
  # shellcheck disable=SC1091
  source .venv/bin/activate
fi
export LOCAL_SD_HOST="${LOCAL_SD_HOST:-127.0.0.1}"
export LOCAL_SD_PORT="${LOCAL_SD_PORT:-8791}"
export LOCAL_SD_API_KEY="${LOCAL_SD_API_KEY:-local-sd-key}"
LOCAL_MODEL_DIR="$(cd "$(dirname "$0")" && pwd)/models/sd-turbo"
if [[ -z "${LOCAL_SD_MODEL:-}" && -f "$LOCAL_MODEL_DIR/model_index.json" ]]; then
  export LOCAL_SD_MODEL="$LOCAL_MODEL_DIR"
else
  export LOCAL_SD_MODEL="${LOCAL_SD_MODEL:-stabilityai/sd-turbo}"
fi
export LOCAL_SD_STEPS="${LOCAL_SD_STEPS:-2}"
export HF_HUB_DISABLE_TELEMETRY=1
export HF_ENDPOINT="${HF_ENDPOINT:-https://hf-mirror.com}"
exec python server.py --host "$LOCAL_SD_HOST" --port "$LOCAL_SD_PORT" --preload
