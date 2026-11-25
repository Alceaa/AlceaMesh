[CmdletBinding()]
param(
    [string]$Version = '',
    [ValidateSet('github', 'domain')]
    [string]$Source = 'github',
    [string]$Notes = '',
    [string]$Out = '',
    [string]$ManifestUrl = 'https://alceacore.ru/alceamesh/latest.json',
    [string]$UpdateBaseUrl = 'https://alceacore.ru/alceamesh',
    [string]$CacheDir = '',
    [switch]$Check,
    [switch]$Publish,
    [switch]$Push,
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$root = Split-Path -Parent $PSScriptRoot
$utf8 = New-Object System.Text.UTF8Encoding($false)
$headers = @{ 'User-Agent' = 'AlceaMesh-publish-update'; 'Accept' = 'application/vnd.github+json' }
if ($env:GITHUB_TOKEN) {
    $headers['Authorization'] = 'Bearer ' + $env:GITHUB_TOKEN
}

function Get-ProjectVersion {
    param([string]$Path)
    $match = Select-String -Path $Path -Pattern '^const Version = "([^"]+)"'
    if (-not $match) {
        throw "Не нашёл строку const Version в $Path"
    }
    return $match.Matches[0].Groups[1].Value
}

function Get-RepoSlug {
    $url = (& git config --get remote.origin.url) 2>$null
    if (-not $url) {
        throw 'Не нашёл remote.origin.url — запускайте скрипт из клона репозитория.'
    }
    if ($url -match 'github\.com[:/](?<owner>[^/]+)/(?<repo>[^/]+?)(\.git)?$') {
        return $Matches['owner'] + '/' + $Matches['repo']
    }
    throw "Адрес репозитория не похож на GitHub: $url"
}

function Invoke-Json {
    param([string]$Uri)
    try {
        return Invoke-RestMethod -Uri $Uri -Headers $headers -TimeoutSec 30
    } catch {
        $response = $_.Exception.Response
        if ($response -and [int]$response.StatusCode -eq 404) {
            return $null
        }
        throw
    }
}

function Get-RemoteSize {
    param([string]$Url)
    try {
        $response = Invoke-WebRequest -Uri $Url -Method Head -UseBasicParsing -TimeoutSec 30
        if ($response.Headers['Content-Length']) {
            return [int64]$response.Headers['Content-Length']
        }
    } catch {
    }
    try {
        $response = Invoke-WebRequest -Uri $Url -Headers @{ Range = 'bytes=0-0' } -UseBasicParsing -TimeoutSec 30
        if ($response.Headers['Content-Range'] -match '/(\d+)$') {
            return [int64]$Matches[1]
        }
        if ($response.Headers['Content-Length']) {
            return [int64]$response.Headers['Content-Length']
        }
    } catch {
    }
    return 0
}

function Save-Text {
    param([string]$Path, [string]$Text)
    $dir = Split-Path -Parent $Path
    if ($dir -and -not (Test-Path $dir)) {
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
    }
    [IO.File]::WriteAllText($Path, $Text, $utf8)
}

Push-Location $root
try {
    if (-not $Version) {
        $Version = Get-ProjectVersion 'internal\version\version.go'
    }
    $numeric = $Version -replace '-.*$', ''
    if ($numeric -notmatch '^\d+(\.\d+){1,3}$') {
        throw "Версия '$Version' не годится: нужен вид 1.2.3 или 1.2.3-m1"
    }

    $slug = Get-RepoSlug
    $tag = "v$Version"
    $assetName = "AlceaMesh-Setup-$Version.exe"
    if (-not $Out) {
        $Out = Join-Path $root 'deploy\updates\latest.json'
    }
    if (-not $CacheDir) {
        $CacheDir = Join-Path $root 'dist'
    }

    Write-Host "AlceaMesh: манифест обновления для $Version"

    Write-Host "==> Релиз $tag на GitHub ($slug)"
    $release = Invoke-Json "https://api.github.com/repos/$slug/releases/tags/$tag"
    if (-not $release) {
        $localInstaller = Join-Path $CacheDir $assetName
        $hasGh = Get-Command gh -ErrorAction SilentlyContinue
        if ($Publish -and -not $DryRun -and $hasGh) {
            if (-not (Test-Path $localInstaller)) {
                throw "Нет файла $localInstaller — сначала соберите установщик: scripts\build-installer.ps1"
            }
            $ghArgs = @('release', 'create', $tag, $localInstaller, '--title', "AlceaMesh $Version")
            if (Test-Path 'RELEASE_NOTES.md') {
                $ghArgs += @('--notes-file', 'RELEASE_NOTES.md')
            } elseif ($Notes) {
                $ghArgs += @('--notes', $Notes)
            } else {
                $ghArgs += '--generate-notes'
            }
            Write-Host "==> Создаю релиз: gh $($ghArgs -join ' ')"
            & gh @ghArgs
            if ($LASTEXITCODE -ne 0) {
                throw "gh release create завершился с кодом $LASTEXITCODE"
            }
            $release = Invoke-Json "https://api.github.com/repos/$slug/releases/tags/$tag"
        }
        if (-not $release) {
            Write-Host "Релиза $tag ещё нет. Сначала опубликуйте установщик, потом вернитесь сюда:" -ForegroundColor Yellow
            Write-Host ""
            Write-Host "  gh release create $tag `"$localInstaller`" --title `"AlceaMesh $Version`" --notes-file RELEASE_NOTES.md"
            Write-Host "  либо через веб: https://github.com/$slug/releases/new (тег $tag, приложить $assetName)"
            Write-Host ""
            if (-not $hasGh) {
                Write-Host '  gh не установлен: winget install GitHub.cli, затем gh auth login' -ForegroundColor Yellow
            } else {
                Write-Host '  или запустите этот скрипт с -Publish — он создаст релиз через gh сам' -ForegroundColor Yellow
            }
            throw "Релиз $tag не найден — манифест не обновляю (иначе клиенты получат 404)"
        }
    }
    if ($release.draft) {
        throw "Релиз $tag существует, но он черновик — опубликуйте его, иначе файл недоступен клиентам"
    }

    $asset = $release.assets | Where-Object { $_.name -eq $assetName } | Select-Object -First 1
    if (-not $asset) {
        $names = ($release.assets | ForEach-Object { $_.name }) -join ', '
        throw "В релизе $tag нет файла $assetName (там: $names)"
    }
    Write-Host ("    файл: {0} ({1:N0} Б, скачиваний: {2})" -f $asset.name, $asset.size, $asset.download_count)

    $target = Join-Path $CacheDir $assetName
    Write-Host '==> Скачиваю из релиза (источник — GitHub)'
    Invoke-WebRequest -Uri $asset.browser_download_url -Headers @{ 'User-Agent' = $headers['User-Agent'] } -OutFile $target -UseBasicParsing -TimeoutSec 900

    $info = Get-Item $target
    if ($asset.size -and $info.Length -ne $asset.size) {
        throw "Размер скачанного файла ($($info.Length)) не совпал с релизом ($($asset.size))"
    }
    $hash = (Get-FileHash $target -Algorithm SHA256).Hash.ToLower()

    if ($Source -eq 'github') {
        $url = "https://github.com/$slug/releases/download/$tag/$assetName"
    } else {
        $url = $UpdateBaseUrl.TrimEnd('/') + '/' + $assetName
    }

    Write-Host '==> Проверяю, что файл уже доступен по ссылке'
    $remoteSize = Get-RemoteSize $url
    if ($remoteSize -le 0) {
        throw "По адресу $url файл недоступен — манифест не обновляю (для -Source github проверьте публикацию релиза, для -Source domain — заливку на сайт)"
    }
    if ($remoteSize -ne $info.Length) {
        throw "По адресу $url размер $remoteSize Б, а должно быть $($info.Length) Б — это другой файл"
    }
    Write-Host "    доступен, размер совпадает ($remoteSize Б)"

    if (-not $Notes) {
        Write-Host '    заметки не заданы (-Notes) — в манифесте будет пусто' -ForegroundColor Yellow
    }
    $manifest = [ordered]@{
        version     = $Version
        url         = $url
        sha256      = $hash
        size        = $info.Length
        notes       = $Notes
        publishedAt = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
    }
    $json = $manifest | ConvertTo-Json -Depth 4
    $json = $json -replace '":  ', '": '
    $json = $json -replace '(?m)^    ', '  '

    if ($DryRun) {
        Write-Host '==> -DryRun: манифест не записываю, был бы такой:'
        Write-Host $json
    } else {
        Save-Text -Path $Out -Text ($json + [Environment]::NewLine)
        Write-Host "==> Манифест записан: $Out"
    }
    if ($Push -and -not $DryRun) {
        $relative = (Resolve-Path $Out).Path.Substring($root.Length).TrimStart('\', '/') -replace '\\', '/'
        $staged = (& git status --porcelain -- $relative) 2>$null
        if (-not $staged) {
            Write-Host '==> Манифест не изменился — коммит не нужен'
        } else {
            & git add $relative
            & git commit -m "${Version}: manifest" | Out-Null
            & git push origin HEAD
            Write-Host "==> Закоммитил и запушил $relative"
        }
    }

    if ($Check) {
        Write-Host '==> Проверка глазами клиента'
        $live = Invoke-Json $ManifestUrl
        if (-not $live) {
            Write-Host "    манифест по адресу $ManifestUrl недоступен — клиенты увидят ошибку сети" -ForegroundColor Yellow
        } else {
            $rows = @(
                @{ Name = 'version'; Live = $live.version; Expected = $Version },
                @{ Name = 'url'; Live = $live.url; Expected = $url },
                @{ Name = 'sha256'; Live = $live.sha256; Expected = $hash },
                @{ Name = 'size'; Live = $live.size; Expected = $info.Length }
            )
            foreach ($row in $rows) {
                $live = [string]$row.Live
                $want = [string]$row.Expected
                if ($row.Name -eq 'sha256') {
                    $same = $live.ToLower() -eq $want.ToLower()
                } else {
                    $same = $live -eq $want
                }
                if ($same) {
                    Write-Host ("    {0,-8} совпадает" -f $row.Name)
                } else {
                    Write-Host ("    {0,-8} РАСХОДИТСЯ: опубликовано '{1}', ожидалось '{2}'" -f $row.Name, $live, $want) -ForegroundColor Yellow
                }
            }
            $liveSize = Get-RemoteSize ([string]$live.url)
            if ($liveSize -le 0) {
                Write-Host '    файл из опубликованного манифеста недоступен — клиенты не смогут обновиться' -ForegroundColor Yellow
            } else {
                Write-Host ("    файл из опубликованного манифеста доступен ({0:N0} Б)" -f $liveSize)
            }
        }
    }

    Write-Host ''
    Write-Host "Версия:   $Version"
    Write-Host "Ссылка:   $url"
    Write-Host "SHA-256:  $hash"
    Write-Host "Размер:   $($info.Length) Б"
    Write-Host "Манифест: $Out"
    if (-not $Push -and -not $DryRun) {
        Write-Host ''
        Write-Host 'Манифест не запушен: выложите его и запустите с -Push, иначе клиенты не увидят обновление'
    }
} finally {
    Pop-Location
}


