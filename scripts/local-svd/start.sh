#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"
# 复用 local-sd 的 torch/diffusers 环境
VENV="${LOCAL_SD_VENV:-$ROOT/../local-sd/.venv}"
if [[ ! -d "$VENV" ]]; then
  echo "missing venv: $VENV (先部署 scripts/local-sd)" >&2
  exit 1
fi
# shellcheck disable=SC1091
source "$VENV/bin/activate"
export LOCAL_SVD_HOST="${LOCAL_SVD_HOST:-127.0.0.1}"
export LOCAL_SVD_PORT="${LOCAL_SVD_PORT:-8792}"
export LOCAL_SVD_API_KEY="${LOCAL_SVD_API_KEY:-local-svd-key}"
# motion=秒级 Ken Burns（默认，接 gpt2api）；svd=真 SVD（M4 仍需数分钟）
export LOCAL_VIDEO_ENGINE="${LOCAL_VIDEO_ENGINE:-motion}"
# SVD 快档（仅 ENGINE=svd 时生效）
export LOCAL_SVD_STEPS="${LOCAL_SVD_STEPS:-4}"
export LOCAL_SVD_FRAMES="${LOCAL_SVD_FRAMES:-8}"
export LOCAL_SVD_FPS="${LOCAL_SVD_FPS:-6}"
export LOCAL_SVD_DECODE_CHUNK="${LOCAL_SVD_DECODE_CHUNK:-2}"
export LOCAL_SVD_MAX_SIDE="${LOCAL_SVD_MAX_SIDE:-384}"
export LOCAL_SVD_WIDTH="${LOCAL_SVD_WIDTH:-384}"
export LOCAL_SVD_HEIGHT="${LOCAL_SVD_HEIGHT:-256}"
export LOCAL_SVD_MOTION="${LOCAL_SVD_MOTION:-80}"
LOCAL_MODEL_DIR="$ROOT/models/svd-xt"
if [[ -z "${LOCAL_SVD_MODEL:-}" && -f "$LOCAL_MODEL_DIR/model_index.json" ]]; then
  export LOCAL_SVD_MODEL="$LOCAL_MODEL_DIR"
else
  export LOCAL_SVD_MODEL="${LOCAL_SVD_MODEL:-stabilityai/stable-video-diffusion-img2vid-xt}"
fi
export HF_HUB_DISABLE_TELEMETRY=1
export HF_ENDPOINT="${HF_ENDPOINT:-https://hf-mirror.com}"
export PYTORCH_MPS_HIGH_WATERMARK_RATIO="${PYTORCH_MPS_HIGH_WATERMARK_RATIO:-0.0}"
if [[ -d "$LOCAL_SVD_MODEL" ]]; then
  export HF_HUB_OFFLINE=1
  export TRANSFORMERS_OFFLINE=1
fi
PRELOAD_FLAG=()
if [[ "${LOCAL_VIDEO_ENGINE}" == "svd" ]]; then
  PRELOAD_FLAG=(--preload)
fi
echo "[local-svd] engine=${LOCAL_VIDEO_ENGINE} port=${LOCAL_SVD_PORT}" >&2
if ((${#PRELOAD_FLAG[@]})); then
  exec python "$ROOT/server.py" --host "$LOCAL_SVD_HOST" --port "$LOCAL_SVD_PORT" "${PRELOAD_FLAG[@]}"
else
  exec python "$ROOT/server.py" --host "$LOCAL_SVD_HOST" --port "$LOCAL_SVD_PORT"
fi
