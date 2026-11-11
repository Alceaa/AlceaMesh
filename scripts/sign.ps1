[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Path,
    [string]$Thumbprint = $env:ALCEAMESH_CERT_THUMBPRINT,
    [string]$Pfx = $env:ALCEAMESH_CERT_PFX,
    [string]$PfxPassword = $env:ALCEAMESH_CERT_PASSWORD,
    [string]$Timestamp = 'http://timestamp.digicert.com',
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'

function Find-SignTool {
    $candidates = @()

    $kits = Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10\bin'
    if (Test-Path $kits) {
        $candidates += Get-ChildItem -Path $kits -Directory -ErrorAction SilentlyContinue |
            Sort-Object Name -Descending |
            ForEach-Object { Join-Path $_.FullName 'x64\signtool.exe' }
    }

    $fromPath = (Get-Command 'signtool.exe' -ErrorAction SilentlyContinue).Source
    if ($fromPath) {
        $candidates += $fromPath
    }

    return ($candidates | Where-Object { $_ -and (Test-Path $_) } | Select-Object -First 1)
}

$targets = @()
$resolved = Get-Item -Path $Path -ErrorAction SilentlyContinue
if ($resolved -and $resolved.PSIsContainer) {
    $targets = Get-ChildItem -Path $resolved.FullName -File -Include '*.exe', '*.dll' -Recurse |
        ForEach-Object { $_.FullName }
} elseif ($resolved) {
    $targets = @($resolved.FullName)
} else {
    $targets = @(Get-ChildItem -Path $Path -File | ForEach-Object { $_.FullName })
}

if (-not $targets) {
    throw "Не найдено ни одного файла для подписи: $Path"
}

if (-not $Thumbprint -and -not $Pfx) {
    throw @'
Нет сертификата для подписи. Укажите один из вариантов:
  -Thumbprint <отпечаток>            сертификат из хранилища (Get-ChildItem Cert:\CurrentUser\My)
  -Pfx <файл.pfx> -PfxPassword <пароль>
  или переменные ALCEAMESH_CERT_THUMBPRINT / ALCEAMESH_CERT_PFX (+ ALCEAMESH_CERT_PASSWORD)
Для реальной раздачи нужен OV/EV-сертификат подписи кода; для проверки подойдёт самоподписанный
(New-SelfSignedCertificate -Type CodeSigningCert -Subject "CN=AlceaMesh Test" -CertStoreLocation Cert:\CurrentUser\My).
'@
}

$signTool = Find-SignTool
if (-not $signTool) {
    throw 'Не найден signtool.exe. Установите Windows SDK (Windows Kits\10\bin\x64\signtool.exe) или добавьте его в PATH.'
}

$arguments = @('sign', '/fd', 'SHA256', '/td', 'SHA256', '/tr', $Timestamp)
if ($Pfx) {
    $arguments += @('/f', $Pfx)
    if ($PfxPassword) {
        $arguments += @('/p', $PfxPassword)
    }
} else {
    $arguments += @('/sha1', $Thumbprint)
}
$arguments += $targets

Write-Host "signtool: $signTool"
Write-Host "сертификат: $(if ($Pfx) { $Pfx } else { $Thumbprint })"
Write-Host 'файлы:'
$targets | ForEach-Object { Write-Host "    $_" }

if ($DryRun) {
    Write-Host ''
    Write-Host 'Команда:'
    Write-Host ("    " + $signTool + ' ' + ($arguments -join ' '))
    return
}

& $signTool @arguments
if ($LASTEXITCODE -ne 0) {
    throw "signtool завершился с кодом $LASTEXITCODE"
}

foreach ($target in $targets) {
    & $signTool verify /pa $target
}
Write-Host 'Подпись готова.'
