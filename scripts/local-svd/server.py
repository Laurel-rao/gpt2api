#!/usr/bin/env python3
"""本机小视频服务：SVD 图生视频 + Seedance 2.0 兼容异步任务 API。"""

from __future__ import annotations

import argparse
import base64
import io
import os
import threading
import time
import uuid
from pathlib import Path
from typing import Any
from urllib.request import urlopen

from fastapi import FastAPI, Header, HTTPException, Request
from fastapi.responses import FileResponse, JSONResponse
from PIL import Image

# torch 仅 SVD 引擎需要；motion 模式可延迟导入失败也不挡启动
try:
    import torch
except Exception:  # pragma: no cover
    torch = None  # type: ignore

ROOT = Path(__file__).resolve().parent
DEFAULT_MODEL = ROOT / "models" / "svd-xt"
MODEL_ID = os.environ.get(
    "LOCAL_SVD_MODEL",
    str(DEFAULT_MODEL) if (DEFAULT_MODEL / "model_index.json").is_file() else "stabilityai/stable-video-diffusion-img2vid-xt",
)
API_KEY = os.environ.get("LOCAL_SVD_API_KEY", "local-svd-key")
HOST = os.environ.get("LOCAL_SVD_HOST", "127.0.0.1")
PORT = int(os.environ.get("LOCAL_SVD_PORT", "8792"))
SD_URL = os.environ.get("LOCAL_SD_URL", "http://127.0.0.1:8791/v1/images/generations")
SD_KEY = os.environ.get("LOCAL_SD_API_KEY", "local-sd-key")
# motion=参考图运镜（秒级，默认）；svd=Stable Video Diffusion（M4 很慢）
ENGINE = os.environ.get("LOCAL_VIDEO_ENGINE", "motion").strip().lower()
# M4 MPS 默认偏激进：少帧 / 少步 / 低分辨率
NUM_FRAMES = int(os.environ.get("LOCAL_SVD_FRAMES", "8"))
FPS = int(os.environ.get("LOCAL_SVD_FPS", "6"))
DECODE_CHUNK = int(os.environ.get("LOCAL_SVD_DECODE_CHUNK", "2"))
MOTION_BUCKET = int(os.environ.get("LOCAL_SVD_MOTION", "80"))
INFER_STEPS = int(os.environ.get("LOCAL_SVD_STEPS", "4"))
MAX_SIDE = int(os.environ.get("LOCAL_SVD_MAX_SIDE", "384"))
FORCE_W = int(os.environ.get("LOCAL_SVD_WIDTH", "384"))
FORCE_H = int(os.environ.get("LOCAL_SVD_HEIGHT", "256"))  # 须 64 对齐；224 会对齐成 192
DATA_DIR = ROOT / "data"
DATA_DIR.mkdir(parents=True, exist_ok=True)

app = FastAPI(title="local-svd", version="0.1.0")
_lock = threading.Lock()
_pipe = None
_device = "cpu"
_tasks: dict[str, dict[str, Any]] = {}


def _auth(authorization: str | None) -> None:
    if not API_KEY:
        return
    token = (authorization or "").removeprefix("Bearer ").strip()
    if token != API_KEY:
        raise HTTPException(status_code=401, detail="invalid api key")


def _pick_device():
    if torch is None:
        return "cpu", None
    if torch.backends.mps.is_available():
        return "mps", torch.float16
    if torch.cuda.is_available():
        return "cuda", torch.float16
    return "cpu", torch.float32


def _load_pipe():
    global _pipe, _device
    if _pipe is not None:
        return _pipe
    if torch is None:
        raise RuntimeError("torch not installed; cannot run SVD engine")
    from diffusers import StableVideoDiffusionPipeline

    device, dtype = _pick_device()
    _device = device
    print(f"[local-svd] loading {MODEL_ID} on {device} ({dtype})…", flush=True)
    pipe = StableVideoDiffusionPipeline.from_pretrained(
        MODEL_ID,
        torch_dtype=dtype,
        variant="fp16" if dtype == torch.float16 else None,
        local_files_only=Path(MODEL_ID).is_dir(),
    )
    pipe.to(device)
    if device == "mps":
        # 注意：勿单独把 VAE 转 float32（latent 仍是 half → conv bias dtype 冲突）。
        # 低分辨率 + attention slicing 已足够稳住 MPS。
        try:
            pipe.enable_attention_slicing("max")
        except Exception as exc:
            print(f"[local-svd] attention slicing skipped: {exc}", flush=True)
        try:
            # 部分 diffusers 版本支持；失败可忽略
            pipe.vae.enable_forward_chunking(chunk_size=1, dim=0)  # type: ignore[attr-defined]
        except Exception:
            pass
    _pipe = pipe
    print(
        f"[local-svd] model ready frames={NUM_FRAMES} steps={INFER_STEPS} "
        f"size={FORCE_W}x{FORCE_H} decode_chunk={DECODE_CHUNK}",
        flush=True,
    )
    return _pipe


def _decode_image(ref: str) -> Image.Image:
    data = ref.strip()
    if data.startswith("http://") or data.startswith("https://"):
        with urlopen(data, timeout=60) as resp:
            raw = resp.read()
        return Image.open(io.BytesIO(raw)).convert("RGB")
    if data.startswith("data:"):
        data = data.split(",", 1)[-1]
    raw = base64.b64decode(data)
    return Image.open(io.BytesIO(raw)).convert("RGB")


def _extract_prompt_and_image(body: dict[str, Any]) -> tuple[str, Image.Image | None]:
    prompt = ""
    image = None
    content = body.get("content")
    if isinstance(content, list):
        for item in content:
            if not isinstance(item, dict):
                continue
            t = item.get("type")
            if t == "text":
                prompt = str(item.get("text") or prompt)
            elif t in {"image_url", "image"}:
                url = item.get("image_url")
                if isinstance(url, dict):
                    url = url.get("url")
                if isinstance(url, str) and url.strip():
                    try:
                        image = _decode_image(url)
                    except Exception as exc:
                        print(f"[local-svd] skip image ref: {exc}", flush=True)
            elif t == "image_base64" and item.get("image_base64"):
                try:
                    image = _decode_image(str(item["image_base64"]))
                except Exception as exc:
                    print(f"[local-svd] skip image_base64: {exc}", flush=True)
    if not prompt:
        prompt = str(body.get("prompt") or "").strip()
    if image is None:
        for key in ("image", "reference_image_url", "first_frame"):
            val = body.get(key)
            if isinstance(val, str) and val.strip():
                try:
                    image = _decode_image(val)
                    break
                except Exception:
                    pass
        imgs = body.get("images")
        if image is None and isinstance(imgs, list) and imgs:
            try:
                image = _decode_image(str(imgs[0]))
            except Exception:
                pass
    return prompt, image


def _txt2img(prompt: str) -> Image.Image:
    import json
    import urllib.request

    payload = {
        "model": "gpt-image-2",
        "prompt": prompt or "cinematic still frame",
        "n": 1,
        "size": "1024x576",
        "quality": "low",
        "response_format": "b64_json",
        "output_format": "png",
    }
    req = urllib.request.Request(
        SD_URL,
        data=json.dumps(payload).encode(),
        headers={"Authorization": f"Bearer {SD_KEY}", "Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=180) as resp:
        data = json.loads(resp.read().decode())
    b64 = data["data"][0]["b64_json"]
    return Image.open(io.BytesIO(base64.b64decode(b64))).convert("RGB")


def _export_mp4(frames: list[Image.Image], out_path: Path, duration_sec: int) -> None:
    import subprocess
    import tempfile

    work = Path(tempfile.mkdtemp(prefix="svd-frames-"))
    try:
        for i, frame in enumerate(frames):
            frame.save(work / f"f_{i:04d}.png")
        raw = out_path.with_suffix(".raw.mp4")
        cmd = [
            "ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
            "-framerate", str(FPS),
            "-i", str(work / "f_%04d.png"),
            "-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart",
            str(raw),
        ]
        subprocess.check_call(cmd)
        # 循环拉长到目标时长（Seedance 默认约 15s）
        target = max(2, int(duration_sec or 15))
        looped = out_path
        subprocess.check_call([
            "ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
            "-stream_loop", "-1", "-i", str(raw),
            "-t", str(target),
            "-c:v", "libx264", "-pix_fmt", "yuv420p", "-an",
            "-movflags", "+faststart",
            str(looped),
        ])
        raw.unlink(missing_ok=True)
    finally:
        for p in work.glob("*"):
            p.unlink(missing_ok=True)
        work.rmdir()


def _resize_for_svd(image: Image.Image) -> Image.Image:
    image = image.convert("RGB")
    if FORCE_W > 0 and FORCE_H > 0:
        w = max(64, FORCE_W - FORCE_W % 64)
        h = max(64, FORCE_H - FORCE_H % 64)
        return image.resize((w, h), Image.Resampling.LANCZOS)
    # 最长边约束，保持比例后 64 对齐
    image.thumbnail((MAX_SIDE, max(64, MAX_SIDE * 9 // 16)))
    w, h = image.size
    w = max(64, w - w % 64)
    h = max(64, h - h % 64)
    return image.resize((w, h), Image.Resampling.LANCZOS)


def _generate_motion_video(prompt: str, image: Image.Image | None, duration_sec: int) -> Path:
    """参考图运镜：用真实场景图生成可播放 MP4（秒级），非占位彩条。"""
    import subprocess
    import tempfile

    if image is None:
        print(f"[local-svd] motion: no image, txt2img via local SD: {prompt[:80]!r}", flush=True)
        image = _txt2img(prompt)
    image = image.convert("RGB")
    # 输出 9:16 或保持接近输入
    w, h = image.size
    if h >= w:
        tw, th = 720, 1280
    else:
        tw, th = 1280, 720
    image = image.resize((tw, th), Image.Resampling.LANCZOS)
    out = DATA_DIR / f"{uuid.uuid4().hex}.mp4"
    target = max(2, int(duration_sec or 15))
    t0 = time.time()
    with tempfile.TemporaryDirectory(prefix="motion-") as tmp:
        still = Path(tmp) / "still.png"
        image.save(still)
        # Ken Burns：缓慢推进 + 轻微平移
        frames = max(target * 30, 60)
        z_end = 1.18
        vf = (
            f"scale={tw * 2}:{th * 2},"
            f"zoompan=z='min(zoom+0.0008\\,{z_end})':x='iw/2-(iw/zoom/2)':y='ih/2-(ih/zoom/2)':"
            f"d={frames}:s={tw}x{th}:fps=30,"
            f"format=yuv420p"
        )
        subprocess.check_call([
            "ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
            "-loop", "1", "-i", str(still),
            "-vf", vf,
            "-t", str(target),
            "-c:v", "libx264", "-pix_fmt", "yuv420p", "-an",
            "-movflags", "+faststart",
            str(out),
        ])
    print(f"[local-svd] motion wrote {out} in {time.time() - t0:.1f}s prompt={prompt[:60]!r}", flush=True)
    return out


def _generate_svd_video(prompt: str, image: Image.Image | None, duration_sec: int) -> Path:
    if image is None:
        print(f"[local-svd] no image, txt2img via local SD: {prompt[:80]!r}", flush=True)
        image = _txt2img(prompt)
    image = _resize_for_svd(image)
    w, h = image.size

    pipe = _load_pipe()
    out = DATA_DIR / f"{uuid.uuid4().hex}.mp4"
    t0 = time.time()
    step_t = {"t": t0}

    def _on_step(pipe_self, step_idx, timestep, callback_kwargs):  # noqa: ARG001
        now = time.time()
        dt = now - step_t["t"]
        step_t["t"] = now
        print(
            f"[local-svd] step {int(step_idx)+1}/{INFER_STEPS} "
            f"dt={dt:.1f}s elapsed={now - t0:.1f}s",
            flush=True,
        )
        return callback_kwargs

    print(
        f"[local-svd] infer size={w}x{h} frames={NUM_FRAMES} steps={INFER_STEPS} "
        f"decode_chunk={DECODE_CHUNK} device={_device}",
        flush=True,
    )
    with _lock:
        result = pipe(
            image,
            decode_chunk_size=DECODE_CHUNK,
            num_frames=NUM_FRAMES,
            num_inference_steps=INFER_STEPS,
            motion_bucket_id=MOTION_BUCKET,
            noise_aug_strength=0.02,
            callback_on_step_end=_on_step,
        )
        frames = result.frames[0]
    pil_frames = []
    for fr in frames:
        if isinstance(fr, Image.Image):
            pil_frames.append(fr.convert("RGB"))
        else:
            pil_frames.append(Image.fromarray(fr).convert("RGB"))
    _export_mp4(pil_frames, out, duration_sec)
    print(f"[local-svd] wrote {out} in {time.time() - t0:.1f}s frames={len(pil_frames)}", flush=True)
    return out


def _generate_video(prompt: str, image: Image.Image | None, duration_sec: int) -> Path:
    if ENGINE == "svd":
        return _generate_svd_video(prompt, image, duration_sec)
    return _generate_motion_video(prompt, image, duration_sec)


def _public_base(request: Request) -> str:
    host = request.headers.get("host") or f"{HOST}:{PORT}"
    return f"http://{host}"


def _run_task(task_id: str, prompt: str, image: Image.Image | None, duration_sec: int, request_base: str) -> None:
    try:
        _tasks[task_id]["status"] = "running"
        path = _generate_video(prompt, image, duration_sec)
        _tasks[task_id].update({
            "status": "succeeded",
            "content": {"video_url": f"{request_base}/media/{path.name}"},
            "local_path": str(path),
            "finished_at": time.time(),
        })
    except Exception as exc:
        print(f"[local-svd] task {task_id} failed: {exc}", flush=True)
        _tasks[task_id].update({"status": "failed", "error": str(exc), "finished_at": time.time()})


@app.get("/healthz")
def healthz() -> dict[str, Any]:
    return {
        "status": "ok",
        "engine": ENGINE,
        "model": MODEL_ID if ENGINE == "svd" else "motion-kenburns+local-sd",
        "device": (_device if _pipe else _pick_device()[0]) if ENGINE == "svd" else "cpu",
        "loaded": _pipe is not None if ENGINE == "svd" else True,
        "protocol": "seedance-2.0",
    }


@app.post("/seedance/api/v3/contents/generations/tasks")
async def create_task(request: Request, authorization: str | None = Header(default=None)) -> JSONResponse:
    _auth(authorization)
    body = await request.json()
    prompt, image = _extract_prompt_and_image(body)
    duration = int(body.get("duration") or body.get("duration_sec") or 15)
    task_id = f"cgt-{uuid.uuid4().hex[:16]}"
    base = _public_base(request)
    _tasks[task_id] = {
        "id": task_id,
        "model": body.get("model") or "doubao-seedance-2-0-fast-260128",
        "status": "queued",
        "prompt": prompt,
        "created_at": time.time(),
    }
    threading.Thread(
        target=_run_task,
        args=(task_id, prompt, image, duration, base),
        daemon=True,
    ).start()
    return JSONResponse({"id": task_id})


@app.get("/seedance/api/v3/contents/generations/tasks/{task_id}")
def get_task(task_id: str, authorization: str | None = Header(default=None)) -> JSONResponse:
    _auth(authorization)
    task = _tasks.get(task_id)
    if not task:
        raise HTTPException(status_code=404, detail="task not found")
    out = {
        "id": task["id"],
        "model": task.get("model"),
        "status": task["status"],
        "content": task.get("content") or {},
    }
    if task.get("error"):
        out["error"] = task["error"]
    return JSONResponse(out)


@app.get("/media/{name}")
def media(name: str):
    path = DATA_DIR / Path(name).name
    if not path.is_file():
        raise HTTPException(status_code=404, detail="not found")
    return FileResponse(path, media_type="video/mp4")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default=HOST)
    parser.add_argument("--port", type=int, default=PORT)
    parser.add_argument("--preload", action="store_true")
    args = parser.parse_args()
    print(f"[local-svd] engine={ENGINE} port={args.port}", flush=True)
    if args.preload and ENGINE == "svd":
        _load_pipe()
    import uvicorn

    uvicorn.run(app, host=args.host, port=args.port, log_level="info")


if __name__ == "__main__":
    main()
