[CmdletBinding()]
param(
    [string]$Version = '',
    [switch]$SkipApp
)

$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
$utf8 = New-Object System.Text.UTF8Encoding($false)

function Get-ProjectVersion {
    param([string]$Path)
    $match = Select-String -Path $Path -Pattern '^const Version = "([^"]+)"'
    if (-not $match) {
        throw "Не нашёл строку const Version в $Path"
    }
    return $match.Matches[0].Groups[1].Value
}

function Set-YamlVersion {
    param([string]$Path, [string]$Value)
    $full = (Resolve-Path $Path).Path
    $lines = [IO.File]::ReadAllLines($full, [Text.Encoding]::UTF8)
    for ($index = 0; $index -lt $lines.Count; $index++) {
        if ($lines[$index] -match '^(\s+version:\s*)([''"])([^''"]*)([''"])') {
            $found = $Matches[0]
            $lines[$index] = $lines[$index].Replace($found, $Matches[1] + $Matches[2] + $Value + $Matches[4])
            break
        }
    }
    [IO.File]::WriteAllLines($full, $lines, $utf8)
}

function Set-JsonVersion {
    param([string]$Path, [string]$Value)
    $full = (Resolve-Path $Path).Path
    $text = [IO.File]::ReadAllText($full, [Text.Encoding]::UTF8)
    $regex = New-Object System.Text.RegularExpressions.Regex('(?m)^(\s*"version"\s*:\s*)"\d[^"]*"')
    $updated = $regex.Replace($text, ('$1"' + $Value + '"'))
    if ($updated -ne $text) {
        [IO.File]::WriteAllText($full, $updated, $utf8)
    }
}

function Write-InfoJson {
    param([string]$Path, [string]$Numeric, [string]$Release)
    $info = [ordered]@{
        fixed = [ordered]@{
            file_version    = $Numeric
            product_version = $Numeric
        }
        info  = [ordered]@{
            '0409' = [ordered]@{
                FileVersion     = $Numeric
                ProductVersion  = $Numeric
                CompanyName     = 'Alcea'
                FileDescription = 'AlceaMesh — передача файлов и VPN'
                LegalCopyright  = '(c) 2026, Alcea'
                ProductName     = 'AlceaMesh'
                Comments        = "AlceaMesh $Release"
            }
        }
    }
    $json = $info | ConvertTo-Json -Depth 6
    [IO.File]::WriteAllText((Join-Path $root $Path), $json, $utf8)
}

Push-Location $root
try {
    if (-not $Version) {
        $Version = Get-ProjectVersion 'internal\version\version.go'
    }

    $numeric = $Version -replace '-.*$', ''
    if ($numeric -notmatch '^\d+(\.\d+){1,3}$') {
        throw "Версия '$Version' не годится для Windows: нужен вид 1.2.3 или 1.2.3-m1"
    }

    Write-Host "AlceaMesh: сборка установщика $Version"

    Write-Host '==> Версия в файлах сборки'
    Set-YamlVersion -Path 'build\config.yml' -Value $numeric
    Set-JsonVersion -Path 'frontend\package.json' -Value $Version
    Set-JsonVersion -Path 'frontend\package-lock.json' -Value $Version
    Write-InfoJson -Path 'build\windows\info.json' -Numeric $numeric -Release $Version

    Write-Host '==> Иконка'
    Copy-Item 'assets\icon.ico' 'build\windows\icon.ico' -Force

    Write-Host '==> Ресурсы служебного exe (иконка, версия)'
    & wails3 generate syso -arch amd64 -icon 'assets\icon.ico' -manifest 'build\windows\wails.exe.manifest' -info 'build\windows\info.json' -out 'cmd\alceameshd\daemon_windows_amd64.syso'
    if ($LASTEXITCODE -ne 0) {
        throw "generate syso (alceameshd) завершился с кодом $LASTEXITCODE"
    }

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

    Write-Host '==> Иконка и версия установщика'
    & wails3 generate syso -arch amd64 -icon 'assets\icon.ico' -manifest 'build\windows\wails.exe.manifest' -info 'build\windows\info.json' -out 'cmd\alceamesh-setup\setup_windows_amd64.syso'
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
    $hash = (Get-FileHash $out -Algorithm SHA256).Hash
    Write-Host ''
    Write-Host "Готово: $out ($size МБ)"
    Write-Host "SHA-256: $hash"
    Write-Host "Проверить без изменений:  .\$out --quiet --dry-run --dir C:\Temp\AlceaMeshTest"
    Write-Host 'Подписать:                 scripts\sign.ps1 -Path dist'
    Write-Host 'Инструкция по релизу:      docs\release.md'
} finally {
    Pop-Location
}
