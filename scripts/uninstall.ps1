[CmdletBinding()]
param(
    [string]$InstallDir = (Join-Path $env:ProgramFiles 'AlceaMesh'),
    [switch]$PurgeSettings,
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'

$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole(
    [Security.Principal.WindowsBuiltInRole]::Administrator
)
if (-not $isAdmin -and -not $DryRun) {
    throw 'Запустите PowerShell от имени администратора (или добавьте -DryRun, чтобы посмотреть план).'
}

Write-Host "AlceaMesh: удаление из $InstallDir"
if ($DryRun) {
    Write-Host '  режим: только план (-DryRun), ничего не меняется'
}

$daemonExe = Join-Path $InstallDir 'alceameshd.exe'
if (Test-Path $daemonExe) {
    Write-Host '==> Остановка и удаление службы AlceaMesh'
    if ($DryRun) {
        Write-Host "    sc.exe stop AlceaMesh; $daemonExe uninstall"
    } else {
        & sc.exe stop AlceaMesh | Out-Null
        & $daemonExe uninstall
    }
} else {
    Write-Host '==> Служба не найдена в папке установки — пропускаем'
}

Write-Host '==> Правила брандмауэра'
$rules = Get-NetFirewallRule -ErrorAction SilentlyContinue | Where-Object { $_.DisplayName -like 'AlceaMesh (*' }
foreach ($rule in $rules) {
    Write-Host "    $($rule.DisplayName)"
    if (-not $DryRun) {
        Remove-NetFirewallRule -DisplayName $rule.DisplayName
    }
}
if (-not $rules) {
    Write-Host '    (нет)'
}

Write-Host '==> Ярлыки'
$shortcuts = @(
    (Join-Path $env:ProgramData 'Microsoft\Windows\Start Menu\Programs\AlceaMesh.lnk'),
    (Join-Path ([Environment]::GetFolderPath('Desktop')) 'AlceaMesh.lnk')
)
foreach ($shortcut in $shortcuts) {
    if (Test-Path $shortcut) {
        Write-Host "    $shortcut"
        if (-not $DryRun) {
            Remove-Item -LiteralPath $shortcut -Force
        }
    }
}

Write-Host '==> Автозапуск'
if (Get-ItemProperty -Path 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' -Name 'AlceaMesh' -ErrorAction SilentlyContinue) {
    Write-Host '    HKCU:\...\Run\AlceaMesh'
    if (-not $DryRun) {
        Remove-ItemProperty -Path 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' -Name 'AlceaMesh'
    }
} else {
    Write-Host '    (нет)'
}

Write-Host "==> Файлы программы"
if (Test-Path $InstallDir) {
    Write-Host "    $InstallDir"
    if (-not $DryRun) {
        Remove-Item -LiteralPath $InstallDir -Recurse -Force
    }
}

if ($PurgeSettings) {
    $settingsDir = Join-Path $env:AppData 'AlceaMesh'
    Write-Host "==> Настройки ($settingsDir), включая импортированный .conf"
    if (Test-Path $settingsDir) {
        if (-not $DryRun) {
            Remove-Item -LiteralPath $settingsDir -Recurse -Force
        }
    }
} else {
    Write-Host '==> Настройки оставлены (удалить вместе с ними: -PurgeSettings)'
}

Write-Host ''
if ($DryRun) {
    Write-Host 'План проверен. Без -DryRun будут выполнены перечисленные действия.'
} else {
    Write-Host 'Готово.'
}
