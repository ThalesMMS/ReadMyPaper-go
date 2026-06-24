#!/usr/bin/env python3
"""Internal Kokoro bridge used by the Go desktop application."""
from __future__ import annotations

import argparse
import json
import math
import sys
import wave
from pathlib import Path

SAMPLE_RATE = 24000
SAMPLE_WIDTH = 2
CHANNELS = 1


def emit(progress: float, step: str) -> None:
    print(
        "RMP_PROGRESS "
        + json.dumps({"progress": max(0.0, min(1.0, progress)), "step": step}),
        flush=True,
    )


def pause_for(chunk: str) -> int:
    stripped = chunk.rstrip()
    if not stripped:
        return 400
    if stripped[-1] in ".!?":
        return 400
    if stripped[-1] in ",;:":
        return 150
    return 600


def silence(pause_ms: int) -> bytes:
    frames = math.floor(SAMPLE_RATE * max(pause_ms, 0) / 1000)
    return b"\x00" * frames * SAMPLE_WIDTH * CHANNELS


def pcm16(audio) -> bytes:
    import numpy as np

    if not isinstance(audio, np.ndarray):
        audio = audio.cpu().numpy()
    audio = np.asarray(audio).reshape(-1)
    return (np.clip(audio, -1.0, 1.0) * 32767).astype(np.int16).tobytes()


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--request", required=True)
    args = parser.parse_args()
    request = json.loads(Path(args.request).read_text(encoding="utf-8"))

    try:
        from kokoro import KPipeline
    except ImportError as exc:
        raise RuntimeError(
            "kokoro is not installed in this Python environment; run "
            "`python -m pip install kokoro numpy` and install espeak-ng"
        ) from exc

    chunks = [str(value).strip() for value in request.get("chunks", []) if str(value).strip()]
    if not chunks:
        raise ValueError("request contains no text chunks")
    pipeline = KPipeline(lang_code=str(request.get("language") or "a"))
    voice = str(request.get("voice") or "af_heart")
    speed = max(0.5, min(2.0, float(request.get("speed", 1.0))))

    output_path = Path(request["output_path"])
    output_path.parent.mkdir(parents=True, exist_ok=True)
    temporary_path = output_path.with_suffix(output_path.suffix + ".part")
    temporary_path.unlink(missing_ok=True)
    try:
        with wave.open(str(temporary_path), "wb") as wav_file:
            wav_file.setframerate(SAMPLE_RATE)
            wav_file.setsampwidth(SAMPLE_WIDTH)
            wav_file.setnchannels(CHANNELS)
            for index, chunk in enumerate(chunks):
                emitted_audio = False
                for _graphemes, _phonemes, audio in pipeline(chunk, voice=voice, speed=speed):
                    if audio is not None and len(audio) > 0:
                        wav_file.writeframes(pcm16(audio))
                        emitted_audio = True
                if not emitted_audio:
                    raise RuntimeError(f"Kokoro produced no audio for chunk {index + 1}")
                if index < len(chunks) - 1:
                    wav_file.writeframes(silence(pause_for(chunk)))
                emit((index + 1) / len(chunks), f"Synthesizing audio ({index + 1}/{len(chunks)} chunks)")
        temporary_path.replace(output_path)
    except Exception:
        temporary_path.unlink(missing_ok=True)
        raise
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"{type(exc).__name__}: {exc}", file=sys.stderr, flush=True)
        raise SystemExit(1)
