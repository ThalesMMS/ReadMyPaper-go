param(
    [string]$PythonExecutable = "py",
    [string[]]$PythonArguments = @("-3.12"),
    [string]$VenvDir = ".venv"
)

$ErrorActionPreference = "Stop"

& $PythonExecutable @PythonArguments -m venv $VenvDir
$venvPython = Join-Path $VenvDir "Scripts\python.exe"
& $venvPython -m pip install --upgrade pip
& $venvPython -m pip install -r requirements-tts.txt
Write-Host "`nTTS installed. Before running:"
Write-Host ('$env:READMYPAPER_PYTHON_BIN = "' + (Resolve-Path $venvPython) + '"')
