#!/usr/bin/env python3
"""Internal Piper bridge used by the Go desktop application.

The bridge deliberately accepts a JSON file instead of document text on the
command line, avoiding shell quoting and command-length problems.
"""
from __future__ import annotations

import argparse
import json
import math
import sys
import wave
from pathlib import Path


def emit(progress: float, step: str) -> None:
    print(
        "RMP_PROGRESS "
        + json.dumps({"progress": max(0.0, min(1.0, progress)), "step": step}),
        flush=True,
    )


def pause_for(chunk: str, default_ms: int) -> int:
    stripped = chunk.rstrip()
    if not stripped:
        return default_ms
    if stripped[-1] in ".!?":
        return 350
    if stripped[-1] in ",;:":
        return 120
    return 500


def silence(sample_rate: int, sample_width: int, channels: int, pause_ms: int) -> bytes:
    frames = math.floor(sample_rate * max(pause_ms, 0) / 1000)
    return b"\x00" * frames * sample_width * channels


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--request", required=True)
    args = parser.parse_args()
    request = json.loads(Path(args.request).read_text(encoding="utf-8"))

    try:
        from piper import PiperVoice
        from piper.config import SynthesisConfig
    except ImportError as exc:
        raise RuntimeError(
            "piper-tts is not installed in this Python environment; run "
            "`python -m pip install piper-tts`"
        ) from exc

    model_path = str(request["model_path"])
    config_path = str(request.get("config_path") or "")
    try:
        voice = PiperVoice.load(model_path, config_path=config_path or None)
    except TypeError:
        voice = PiperVoice.load(model_path)

    rate = max(0.7, min(1.4, float(request.get("speech_rate", 1.0))))
    syn_config = SynthesisConfig(length_scale=round(1.0 / rate, 3))
    chunks = [str(value).strip() for value in request.get("chunks", []) if str(value).strip()]
    if not chunks:
        raise ValueError("request contains no text chunks")

    output_path = Path(request["output_path"])
    output_path.parent.mkdir(parents=True, exist_ok=True)
    temporary_path = output_path.with_suffix(output_path.suffix + ".part")
    temporary_path.unlink(missing_ok=True)

    sample_rate = None
    sample_width = None
    channels = None
    try:
        with wave.open(str(temporary_path), "wb") as wav_file:
            for index, chunk in enumerate(chunks):
                emitted_audio = False
                for audio_chunk in voice.synthesize(chunk, syn_config=syn_config):
                    if sample_rate is None:
                        sample_rate = int(audio_chunk.sample_rate)
                        sample_width = int(audio_chunk.sample_width)
                        channels = int(audio_chunk.sample_channels)
                        wav_file.setframerate(sample_rate)
                        wav_file.setsampwidth(sample_width)
                        wav_file.setnchannels(channels)
                    wav_file.writeframes(audio_chunk.audio_int16_bytes)
                    emitted_audio = True
                if not emitted_audio:
                    raise RuntimeError(f"Piper produced no audio for chunk {index + 1}")
                if index < len(chunks) - 1 and sample_rate is not None:
                    wav_file.writeframes(
                        silence(
                            sample_rate,
                            sample_width or 2,
                            channels or 1,
                            pause_for(chunk, int(request.get("pause_ms", 220))),
                        )
                    )
                emit((index + 1) / len(chunks), f"Synthesizing audio ({index + 1}/{len(chunks)} chunks)")
        temporary_path.replace(output_path)
    except Exception:
        temporary_path.unlink(missing_ok=True)
        raise
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:  # concise error consumed by the Go UI
        print(f"{type(exc).__name__}: {exc}", file=sys.stderr, flush=True)
        raise SystemExit(1)
