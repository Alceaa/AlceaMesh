[CmdletBinding()]
param(
    [string]$Version = '',
    [switch]$SkipApp
)

$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    if (-not $Version) {
        $match = Select-String -Path 'main.go' -Pattern 'appVersion = "([^"]+)"'
        if (-not $match) {
            throw 'Не нашёл appVersion в main.go'
        }
        $Version = $match.Matches[0].Groups[1].Value
    }

    Write-Host "AlceaMesh: сборка установщика $Version"

    Write-Host '==> Иконка'
    Copy-Item 'assets\icon.ico' 'build\windows\icon.ico' -Force

    if (-not $SkipApp) {
        Write-Host '==> Приложение (wails3 build)'
        & wails3 build
        if ($LASTEXITCODE -ne 0) {
            throw "wails3 build завершился с кодом $LASTEXITCODE"
        }

        Write-Host '==> Служебные бинарники'
        & go build -o 'bin\alceameshd.exe' '.\cmd\alceameshd'
        & go build -o 'bin\alceamesh-cli.exe' '.\cmd\alceamesh-cli'
        & go build -o 'bin\alceacoord.exe' '.\cmd\alceacoord'
    }

    Write-Host '==> Полезная нагрузка установщика'
    $payload = 'cmd\alceamesh-setup\payload'
    New-Item -ItemType Directory -Force -Path $payload | Out-Null
    Remove-Item "$payload\*.exe", "$payload\*.dll" -Force -ErrorAction SilentlyContinue
    Copy-Item 'bin\alceamesh.exe' $payload -Force
    Copy-Item 'bin\alceameshd.exe' $payload -Force
    Copy-Item 'third_party\wintun.dll' $payload -Force

    Write-Host '==> Иконка установщика'
    & wails3 generate syso -arch amd64 -icon 'assets\icon.ico' -manifest 'build\windows\wails.exe.manifest' -out 'cmd\alceamesh-setup\setup_windows_amd64.syso'
    if ($LASTEXITCODE -ne 0) {
        throw "generate syso завершился с кодом $LASTEXITCODE"
    }

    Write-Host '==> Установщик'
    New-Item -ItemType Directory -Force -Path 'dist' | Out-Null
    $out = "dist\AlceaMesh-Setup-$Version.exe"
    & go build -tags production -trimpath -ldflags '-w -s -H windowsgui' -o $out '.\cmd\alceamesh-setup'
    if ($LASTEXITCODE -ne 0) {
        throw "go build завершился с кодом $LASTEXITCODE"
    }

    $size = [Math]::Round((Get-Item $out).Length / 1MB, 1)
    Write-Host ''
    Write-Host "Готово: $out ($size МБ)"
    Write-Host "Проверить без изменений:  .\$out --quiet --dry-run --dir C:\Temp\AlceaMeshTest"
    Write-Host 'Подписать:                 scripts\sign.ps1 -Path dist'
    Write-Host 'Инструкция по релизу:      docs\release.md'
} finally {
    Pop-Location
}
