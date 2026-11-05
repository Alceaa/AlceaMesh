$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    $out = Join-Path $root 'bin'
    New-Item -ItemType Directory -Force -Path $out | Out-Null

    go build -o (Join-Path $out 'alceameshd.exe') '.\cmd\alceameshd'
    go build -o (Join-Path $out 'alceamesh-cli.exe') '.\cmd\alceamesh-cli'
    go build -o (Join-Path $out 'alceacoord.exe') '.\cmd\alceacoord'
    Copy-Item (Join-Path $root 'third_party\wintun.dll') $out -Force

    Write-Host "built: $out"
} finally {
    Pop-Location
}
