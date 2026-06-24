#!/usr/bin/env sh
set -eu

PYTHON_BIN="${PYTHON_BIN:-python3}"
VENV_DIR="${VENV_DIR:-.venv}"

"$PYTHON_BIN" -m venv "$VENV_DIR"
# shellcheck disable=SC1091
. "$VENV_DIR/bin/activate"
python -m pip install --upgrade pip
python -m pip install -r requirements-tts.txt
printf '\nTTS installed. Before running:\n  export READMYPAPER_PYTHON_BIN="%s/bin/python"\n' "$PWD/$VENV_DIR"
