$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    $wails3 = Join-Path (go env GOPATH) 'bin\wails3.exe'
    if (-not (Test-Path $wails3)) {
        $wails3 = 'wails3'
    }

    & $wails3 build

    Write-Host "built: $(Join-Path $root 'bin\alceamesh.exe')"
} finally {
    Pop-Location
}
