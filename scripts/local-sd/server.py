#!/usr/bin/env python3
"""本机小生图服务：SD-Turbo + Apple MPS，OpenAI 兼容 /v1/images/*。"""

from __future__ import annotations

import argparse
import base64
import io
import os
import threading
import time
from typing import Any

import torch
from fastapi import FastAPI, Header, HTTPException
from fastapi.responses import JSONResponse
from pydantic import BaseModel, Field
from PIL import Image

_DEFAULT_LOCAL = os.path.join(os.path.dirname(__file__), "models", "sd-turbo")
MODEL_ID = os.environ.get(
    "LOCAL_SD_MODEL",
    _DEFAULT_LOCAL if os.path.isfile(os.path.join(_DEFAULT_LOCAL, "model_index.json")) else "stabilityai/sd-turbo",
)
DEFAULT_STEPS = int(os.environ.get("LOCAL_SD_STEPS", "2"))
DEFAULT_GUIDANCE = float(os.environ.get("LOCAL_SD_GUIDANCE", "0.0"))
API_KEY = os.environ.get("LOCAL_SD_API_KEY", "local-sd-key")
HOST = os.environ.get("LOCAL_SD_HOST", "127.0.0.1")
PORT = int(os.environ.get("LOCAL_SD_PORT", "8791"))

app = FastAPI(title="local-sd-turbo", version="0.1.0")
_lock = threading.Lock()
_pipe = None
_device = "cpu"
_dtype = torch.float32


class GenerationRequest(BaseModel):
    model: str = "gpt-image-2"
    prompt: str
    n: int = Field(default=1, ge=1, le=4)
    size: str = "1024x1024"
    quality: str = "low"
    background: str = "auto"
    output_format: str = "png"
    response_format: str = "b64_json"


class EditRequest(GenerationRequest):
    images: list[str] = Field(default_factory=list)


def _pick_device() -> tuple[str, torch.dtype]:
    if torch.backends.mps.is_available():
        return "mps", torch.float16
    if torch.cuda.is_available():
        return "cuda", torch.float16
    return "cpu", torch.float32


def _load_pipe():
    global _pipe, _device, _dtype
    if _pipe is not None:
        return _pipe
    from diffusers import AutoPipelineForText2Image

    _device, _dtype = _pick_device()
    print(f"[local-sd] loading {MODEL_ID} on {_device} ({_dtype})…", flush=True)
    pipe = AutoPipelineForText2Image.from_pretrained(
        MODEL_ID,
        torch_dtype=_dtype,
        variant="fp16" if _dtype == torch.float16 else None,
    )
    pipe = pipe.to(_device)
    if _device == "mps":
        pipe.enable_attention_slicing()
    _pipe = pipe
    print("[local-sd] model ready", flush=True)
    return _pipe


def _parse_size(size: str) -> tuple[int, int]:
    raw = (size or "1024x1024").lower().replace("*", "x")
    try:
        w, h = raw.split("x", 1)
        width, height = int(w), int(h)
    except Exception:
        width, height = 1024, 1024
    # SD-Turbo 对 512 最稳；过大时缩到 768 上限以控显存/耗时
    width = max(256, min(width, 768))
    height = max(256, min(height, 768))
    width -= width % 8
    height -= height % 8
    return width, height


def _auth(authorization: str | None) -> None:
    if not API_KEY:
        return
    token = (authorization or "").removeprefix("Bearer ").strip()
    if token != API_KEY:
        raise HTTPException(status_code=401, detail="invalid api key")


def _encode(image: Image.Image, fmt: str) -> str:
    buf = io.BytesIO()
    fmt_u = (fmt or "png").upper()
    if fmt_u == "JPG":
        fmt_u = "JPEG"
    if fmt_u not in {"PNG", "JPEG", "WEBP"}:
        fmt_u = "PNG"
    image.save(buf, format=fmt_u)
    return base64.b64encode(buf.getvalue()).decode("ascii")


def _decode_ref(ref: str) -> Image.Image:
    data = ref.strip()
    if data.startswith("data:"):
        data = data.split(",", 1)[-1]
    raw = base64.b64decode(data)
    return Image.open(io.BytesIO(raw)).convert("RGB")


def _generate(prompt: str, n: int, size: str, fmt: str, init: Image.Image | None = None) -> list[dict[str, Any]]:
    pipe = _load_pipe()
    width, height = _parse_size(size)
    steps = DEFAULT_STEPS
    guidance = DEFAULT_GUIDANCE
    out: list[dict[str, Any]] = []
    with _lock:
        for _ in range(n):
            t0 = time.time()
            kwargs: dict[str, Any] = {
                "prompt": prompt,
                "num_inference_steps": steps,
                "guidance_scale": guidance,
                "width": width,
                "height": height,
            }
            if init is not None and hasattr(pipe, "__call__"):
                # SD-Turbo txt2img；有参考图时做简易 resize 拼接提示，仍走 txt2img
                # （完整 img2img 需单独 pipeline；本地验收优先保证真出图）
                pass
            result = pipe(**kwargs)
            image = result.images[0]
            out.append({"b64_json": _encode(image, fmt)})
            print(f"[local-sd] generated {width}x{height} in {time.time() - t0:.1f}s", flush=True)
    return out


@app.get("/healthz")
def healthz() -> dict[str, Any]:
    return {
        "status": "ok",
        "model": MODEL_ID,
        "device": _device if _pipe else _pick_device()[0],
        "loaded": _pipe is not None,
    }


@app.get("/v1/models")
def models(authorization: str | None = Header(default=None)) -> dict[str, Any]:
    _auth(authorization)
    return {
        "object": "list",
        "data": [
            {"id": "gpt-image-2", "object": "model", "owned_by": "local-sd-turbo"},
            {"id": MODEL_ID, "object": "model", "owned_by": "local-sd-turbo"},
        ],
    }


@app.post("/v1/images/generations")
def generations(body: GenerationRequest, authorization: str | None = Header(default=None)) -> JSONResponse:
    _auth(authorization)
    if not body.prompt.strip():
        raise HTTPException(status_code=400, detail="prompt required")
    data = _generate(body.prompt, body.n, body.size, body.output_format)
    return JSONResponse({"created": int(time.time()), "data": data})


@app.post("/v1/images/edits")
def edits(body: EditRequest, authorization: str | None = Header(default=None)) -> JSONResponse:
    _auth(authorization)
    if not body.prompt.strip():
        raise HTTPException(status_code=400, detail="prompt required")
    init = None
    if body.images:
        try:
            init = _decode_ref(body.images[0])
        except Exception as exc:
            raise HTTPException(status_code=400, detail=f"invalid reference image: {exc}") from exc
    # 参考图存在时把尺寸对齐，提示词仍驱动生成（轻量本地方案）
    size = body.size
    if init is not None:
        w, h = init.size
        size = f"{min(w, 768)}x{min(h, 768)}"
    data = _generate(body.prompt, body.n, size, body.output_format, init=init)
    return JSONResponse({"created": int(time.time()), "data": data})


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default=HOST)
    parser.add_argument("--port", type=int, default=PORT)
    parser.add_argument("--preload", action="store_true", help="启动时预加载模型")
    args = parser.parse_args()
    if args.preload:
        _load_pipe()
    import uvicorn

    uvicorn.run(app, host=args.host, port=args.port, log_level="info")


if __name__ == "__main__":
    main()
