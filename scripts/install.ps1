[CmdletBinding()]
param(
    [string]$Source = '',
    [string]$InstallDir = (Join-Path $env:ProgramFiles 'AlceaMesh'),
    [switch]$SkipService,
    [switch]$SkipFirewall,
    [switch]$SkipShortcuts,
    [switch]$AutoStart,
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
if (-not $Source) {
    $Source = Join-Path $root 'bin'
}

$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole(
    [Security.Principal.WindowsBuiltInRole]::Administrator
)
if (-not $isAdmin -and -not $DryRun) {
    throw 'Запустите PowerShell от имени администратора (или добавьте -DryRun, чтобы посмотреть план).'
}

$required = @('alceamesh.exe', 'alceameshd.exe', 'wintun.dll')
foreach ($file in $required) {
    if (-not (Test-Path (Join-Path $Source $file))) {
        throw "Не найден файл $file в $Source. Сначала соберите проект: wails3 build и scripts\build.ps1"
    }
}

Write-Host "AlceaMesh: установка в $InstallDir"
Write-Host "  источник: $Source"
if ($DryRun) {
    Write-Host '  режим: только план (-DryRun), ничего не меняется'
}

Write-Host '==> Копирование файлов'
foreach ($file in $required) {
    Write-Host "    $file -> $InstallDir"
    if (-not $DryRun) {
        New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
        Copy-Item -Path (Join-Path $Source $file) -Destination $InstallDir -Force
    }
}

$appExe = Join-Path $InstallDir 'alceamesh.exe'
$daemonExe = Join-Path $InstallDir 'alceameshd.exe'

if (-not $SkipService) {
    Write-Host '==> Служба AlceaMesh (привилегированная часть, автозапуск)'
    if ($DryRun) {
        Write-Host "    $daemonExe install"
    } else {
        & $daemonExe install
    }
}

if (-not $SkipFirewall) {
    Write-Host '==> Правила брандмауэра (входящий и исходящий UDP для P2P и канала через сервер)'
    foreach ($program in @($appExe, $daemonExe)) {
        foreach ($direction in @('Inbound', 'Outbound')) {
            $name = 'AlceaMesh (' + (Split-Path $program -Leaf) + ', ' + $direction + ')'
            Write-Host "    $name"
            if (-not $DryRun) {
                Get-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue | Remove-NetFirewallRule
                New-NetFirewallRule -DisplayName $name -Direction $direction -Action Allow -Program $program -Protocol UDP | Out-Null
            }
        }
    }
}

if (-not $SkipShortcuts) {
    Write-Host '==> Ярлыки'
    $shell = New-Object -ComObject WScript.Shell
    $startMenu = Join-Path $env:ProgramData 'Microsoft\Windows\Start Menu\Programs\AlceaMesh.lnk'
    $targets = @($startMenu, (Join-Path ([Environment]::GetFolderPath('Desktop')) 'AlceaMesh.lnk'))
    foreach ($target in $targets) {
        Write-Host "    $target"
        if (-not $DryRun) {
            $shortcut = $shell.CreateShortcut($target)
            $shortcut.TargetPath = $appExe
            $shortcut.WorkingDirectory = $InstallDir
            $shortcut.Description = 'AlceaMesh — передача файлов и VPN'
            $shortcut.Save()
        }
    }
}

if ($AutoStart) {
    Write-Host '==> Автозапуск при входе в систему'
    Write-Host "    HKCU:\Software\Microsoft\Windows\CurrentVersion\Run\AlceaMesh = \"$appExe\" --tray"
    if (-not $DryRun) {
        Set-ItemProperty -Path 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' -Name 'AlceaMesh' -Value ('"' + $appExe + '" --tray')
    }
}

Write-Host ''
if ($DryRun) {
    Write-Host 'План проверен. Без -DryRun будут выполнены перечисленные действия.'
} else {
    Write-Host 'Готово. Запустите AlceaMesh из меню «Пуск». Автозапуск можно включить в настройках приложения.'
}
