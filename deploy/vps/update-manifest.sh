#!/usr/bin/env bash
set -euo pipefail

REPO="Alceaa/AlceaMesh"
VERSION=""
WEB_DIR="/var/www/alceacore/alceamesh"
URL_KIND="github"
BASE_URL="https://alceacore.ru/alceamesh"
MANIFEST_URL="https://alceacore.ru/alceamesh/latest.json"
NOTES=""
WORK_DIR=""
KEEP_ASSET=0
DRY_RUN=0
CHECK=0
QUIET=0
SKIP_VERIFY=0
INSTALL_TIMER=0
UNINSTALL_TIMER=0
UNIT_NAME="alceamesh-manifest"
TOKEN_FILE="/etc/alceamesh/github.token"
TOKEN="${GITHUB_TOKEN:-}"

log() { [ "$QUIET" = 1 ] || printf '%s\n' "$*"; }
warn() { printf '%s\n' "$*" >&2; }
die() { printf 'ошибка: %s\n' "$*" >&2; exit 1; }

usage() {
    cat <<'TXT'
Обновляет манифест обновлений AlceaMesh (latest.json) прямо на сервере.

Использование: update-manifest.sh [параметры]

  --version X          версия (по умолчанию — последний релиз GitHub)
  --repo owner/repo    репозиторий (по умолчанию Alceaa/AlceaMesh)
  --web-dir PATH       куда писать latest.json (по умолчанию /var/www/alceacore/alceamesh)
  --url github|domain  какая ссылка попадёт в манифест (по умолчанию github)
  --base-url URL       префикс для --url domain (по умолчанию https://alceacore.ru/alceamesh)
  --manifest-url URL   что проверять после записи
  --notes TEXT         заметки для пользователя (по умолчанию — описание релиза с GitHub)
  --keep-asset         положить установщик рядом с манифестом (нужно для --url domain)
  --work-dir PATH      где качать установщик (по умолчанию системный временный каталог)
  --skip-verify        не проверять манифест через сеть
  --dry-run            показать результат, ничего не записывать
  --check              только отчитаться о текущем состоянии
  --quiet              меньше вывода
  --install-timer      поставить systemd-таймер (каждые 15 минут), нужен root
  --uninstall-timer    убрать systemd-таймер, нужен root
  -h, --help           эта справка

Ключ GitHub (если упрётесь в лимит запросов): переменная GITHUB_TOKEN
или файл /etc/alceamesh/github.token.
TXT
}

while [ $# -gt 0 ]; do
    case "$1" in
        --version) VERSION="${2:-}"; shift 2 ;;
        --repo) REPO="${2:-}"; shift 2 ;;
        --web-dir) WEB_DIR="${2:-}"; shift 2 ;;
        --url) URL_KIND="${2:-}"; shift 2 ;;
        --base-url) BASE_URL="${2:-}"; shift 2 ;;
        --manifest-url) MANIFEST_URL="${2:-}"; shift 2 ;;
        --notes) NOTES="${2:-}"; shift 2 ;;
        --work-dir) WORK_DIR="${2:-}"; shift 2 ;;
        --keep-asset) KEEP_ASSET=1; shift ;;
        --skip-verify) SKIP_VERIFY=1; shift ;;
        --dry-run) DRY_RUN=1; shift ;;
        --check) CHECK=1; shift ;;
        --quiet) QUIET=1; shift ;;
        --install-timer) INSTALL_TIMER=1; shift ;;
        --uninstall-timer) UNINSTALL_TIMER=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) die "неизвестный параметр: $1 (см. --help)" ;;
    esac
done

case "$URL_KIND" in
    github|domain) ;;
    *) die "--url должен быть github или domain" ;;
esac

have_jq=0
if command -v jq >/dev/null 2>&1 && jq --version >/dev/null 2>&1; then
    have_jq=1
fi
have_python=0
if command -v python3 >/dev/null 2>&1 && python3 -c 'pass' >/dev/null 2>&1; then
    have_python=1
fi
sha256_cmd=""

require_tools() {
    command -v curl >/dev/null 2>&1 || die "нужен curl (apt-get install -y curl)"
    if command -v sha256sum >/dev/null 2>&1; then
        sha256_cmd="sha256sum"
    elif command -v shasum >/dev/null 2>&1; then
        sha256_cmd="shasum -a 256"
    else
        die "нужен sha256sum (пакет coreutils)"
    fi
    if [ "$have_jq" = 0 ] && [ "$have_python" = 0 ]; then
        die "нужен рабочий jq или python3 (apt-get install -y jq). Наличие в PATH проверяется запуском: заглушки-алиасы не считаются"
    fi
}

file_size() {
    if stat --version >/dev/null 2>&1; then
        stat -c %s "$1"
    else
        wc -c < "$1" | tr -d ' '
    fi
}

json_get() {
    if [ "$have_jq" = 1 ]; then
        jq -r --arg k "$2" '.[$k] // "" | tostring' "$1"
    else
        KEY="$2" python3 -c 'import json, os, sys
data = json.load(open(sys.argv[1], encoding="utf-8"))
value = data.get(os.environ["KEY"], "")
print(value if isinstance(value, str) else json.dumps(value, ensure_ascii=False))' "$1"
    fi
}

flatten_release() {
    if [ "$have_jq" = 1 ]; then
        jq --arg name "$2" '{
            tag: (.tag_name // ""),
            assetName: $name,
            assetSize: ([.assets[] | select(.name == $name) | .size][0] // 0),
            assetUrl: ([.assets[] | select(.name == $name) | .browser_download_url][0] // ""),
            notes: (.body // "")
        }' "$1" > "$3"
    else
        ASSET="$2" python3 -c 'import json, os, sys
data = json.load(open(sys.argv[1], encoding="utf-8"))
name = os.environ["ASSET"]
asset = next((item for item in data.get("assets", []) if item.get("name") == name), None)
flat = {
    "tag": data.get("tag_name") or "",
    "assetName": name,
    "assetSize": (asset or {}).get("size", 0),
    "assetUrl": (asset or {}).get("browser_download_url", ""),
    "notes": data.get("body") or "",
}
json.dump(flat, open(sys.argv[2], "w", encoding="utf-8"), ensure_ascii=False)' "$1" "$3"
    fi
}

build_manifest() {
    if [ "$have_jq" = 1 ]; then
        jq -n --arg version "$M_VERSION" --arg url "$M_URL" --arg sha256 "$M_SHA" \
              --arg notes "$M_NOTES" --arg published "$M_PUBLISHED" --argjson size "$M_SIZE" \
              '{version: $version, url: $url, sha256: $sha256, size: $size, notes: $notes, publishedAt: $published}'
    else
        python3 -c 'import json, os
order = ["version", "url", "sha256", "size", "notes", "publishedAt"]
data = {
    "version": os.environ["M_VERSION"],
    "url": os.environ["M_URL"],
    "sha256": os.environ["M_SHA"],
    "size": int(os.environ["M_SIZE"]),
    "notes": os.environ["M_NOTES"],
    "publishedAt": os.environ["M_PUBLISHED"],
}
print(json.dumps({key: data[key] for key in order}, indent=2, ensure_ascii=False))'
    fi
}

api_get() {
    if [ -n "$TOKEN" ]; then
        curl -fsS -H 'User-Agent: alceamesh-manifest' -H 'Accept: application/vnd.github+json' \
             -H "Authorization: Bearer $TOKEN" "$1"
    else
        curl -fsS -H 'User-Agent: alceamesh-manifest' -H 'Accept: application/vnd.github+json' "$1"
    fi
}

download() {
    curl -fsSL -H 'User-Agent: alceamesh-manifest' -o "$2" "$1"
}


timer_install() {
    [ "$(id -u)" = "0" ] || die "--install-timer требует root"
    command -v systemctl >/dev/null 2>&1 || die "systemd не найден"
    self="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"
    [ -f "$self" ] || die "не нашёл сам себя: $self"

    cat > "/etc/systemd/system/$UNIT_NAME.service" <<EOF
[Unit]
Description=AlceaMesh: обновление манифеста обновлений
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
EnvironmentFile=-/etc/alceamesh/github.env
ExecStart=$self --web-dir $WEB_DIR --url $URL_KIND --base-url $BASE_URL --manifest-url $MANIFEST_URL --quiet
EOF

    cat > "/etc/systemd/system/$UNIT_NAME.timer" <<EOF
[Unit]
Description=AlceaMesh: проверять новые релизы каждые 15 минут

[Timer]
OnBootSec=3min
OnUnitActiveSec=15min
AccuracySec=1min
Persistent=true
Unit=$UNIT_NAME.service

[Install]
WantedBy=timers.target
EOF

    systemctl daemon-reload
    systemctl enable --now "$UNIT_NAME.timer"
    log "Таймер установлен. Проверить: systemctl list-timers $UNIT_NAME.timer"
    log "Журнал: journalctl -u $UNIT_NAME.service -n 20"
}

timer_uninstall() {
    [ "$(id -u)" = "0" ] || die "--uninstall-timer требует root"
    systemctl disable --now "$UNIT_NAME.timer" >/dev/null 2>&1 || true
    rm -f "/etc/systemd/system/$UNIT_NAME.service" "/etc/systemd/system/$UNIT_NAME.timer"
    systemctl daemon-reload
    log "Таймер убран"
}

remote_size() {
    headers="$(curl -fsSL -I -H 'User-Agent: alceamesh-manifest' "$1" 2>/dev/null)" || return 1
    value="$(printf '%s' "$headers" | tr -d '\r' | awk 'tolower($1) == "content-length:" { size = $2 } END { print size }')"
    [ -n "$value" ] || return 1
    printf '%s' "$value"
}

lower() {
    printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

truncate_text() {
    if [ "$have_jq" = 1 ]; then
        jq -rn --arg s "$1" --argjson n "$2" '$s[0:$n]'
    else
        TEXT="$1" LIMIT="$2" python3 -c 'import os
print(os.environ["TEXT"][:int(os.environ["LIMIT"])])'
    fi
}

if [ "$UNINSTALL_TIMER" = 1 ]; then
    timer_uninstall
    exit 0
fi
if [ "$INSTALL_TIMER" = 1 ]; then
    timer_install
    exit 0
fi

require_tools
if [ -z "$TOKEN" ] && [ -r "$TOKEN_FILE" ]; then
    TOKEN="$(tr -d '\r\n' < "$TOKEN_FILE")"
fi

work_root="${WORK_DIR:-${TMPDIR:-/tmp}}"
mkdir -p "$work_root"
work="$(mktemp -d "$work_root/alceamesh-manifest.XXXXXX")"
trap 'rm -rf "$work"' EXIT

log "AlceaMesh: манифест обновлений ($REPO)"

if [ -n "$VERSION" ]; then
    api="https://api.github.com/repos/$REPO/releases/tags/v${VERSION#v}"
else
    api="https://api.github.com/repos/$REPO/releases/latest"
fi
api_get "$api" > "$work/release.json" || die "не получил данные релиза с GitHub ($api): проверьте интернет и лимит запросов (можно задать GITHUB_TOKEN)"

tag="$(json_get "$work/release.json" tag_name)"
[ -n "$tag" ] || die "GitHub не вернул тег релиза ($api)"
version="${tag#v}"
asset="AlceaMesh-Setup-$version.exe"

flatten_release "$work/release.json" "$asset" "$work/flat.json"
size="$(json_get "$work/flat.json" assetSize)"
url_gh="$(json_get "$work/flat.json" assetUrl)"
body="$(json_get "$work/flat.json" notes)"
[ -n "$url_gh" ] || die "в релизе $tag нет файла $asset"

target="$WEB_DIR/latest.json"
current_version=""
current_size=""
current_url=""
current_sha=""
current_notes=""
if [ -f "$target" ]; then
    current_version="$(json_get "$target" version)"
    current_size="$(json_get "$target" size)"
    current_url="$(json_get "$target" url)"
    current_sha="$(json_get "$target" sha256)"
    current_notes="$(json_get "$target" notes)"
fi

if [ "$CHECK" = 1 ]; then
    log "релиз GitHub:      $tag ($asset, $size Б)"
    if [ -f "$target" ]; then
        log "манифест в $WEB_DIR: версия '${current_version:-нет}', размер '${current_size:-нет}'"
    else
        warn "манифеста нет: $target"
    fi
    if curl -fsS "$MANIFEST_URL" -o "$work/live.json" 2>/dev/null; then
        live_version="$(json_get "$work/live.json" version)"
        live_size="$(json_get "$work/live.json" size)"
        live_hash="$(json_get "$work/live.json" sha256)"
        live_url="$(json_get "$work/live.json" url)"
        log "видно снаружи:     версия '$live_version', размер '$live_size' ($MANIFEST_URL)"
        if [ "$live_version" = "$version" ] && [ "$live_size" = "$size" ]; then
            log "состояние: обновление не нужно"
        else
            log "состояние: нужно обновить (в манифесте '$live_version', на GitHub '$version')"
        fi
        if [ -n "$live_url" ] && [ "$SKIP_VERIFY" = 0 ]; then
            if live_remote="$(remote_size "$live_url")"; then
                log "файл из манифеста доступен ($live_remote Б)"
                [ -z "$live_size" ] || [ "$live_remote" = "$live_size" ] || warn "размер файла ($live_remote) не совпал с манифестом ($live_size)"
            else
                warn "файл из манифеста недоступен: $live_url"
            fi
        fi
        [ -n "$live_hash" ] || warn "в опубликованном манифесте нет sha256"
    else
        warn "манифест снаружи недоступен: $MANIFEST_URL"
    fi
    exit 0
fi

if [ "$URL_KIND" = "github" ]; then
    url="$url_gh"
else
    url="${BASE_URL%/}/$asset"
fi

skip_download=0
if [ "$current_version" = "$version" ] && [ "$current_size" = "$size" ] && [ "$current_url" = "$url" ]; then
    if [ -z "$NOTES" ] || [ "$NOTES" = "$current_notes" ]; then
        log "Уже актуально: в манифесте версия $version ($size Б)"
        exit 0
    fi
    log '==> Версия и ссылка те же, меняю только заметки (без скачивания)'
    skip_download=1
    actual_size="$current_size"
    sha_lower="$(lower "$current_sha")"
    [ ${#sha_lower} = 64 ] || die "в текущем манифесте нет корректного sha256 — нужен полный прогон"
fi

if [ "$skip_download" = 0 ]; then
    log "==> Скачиваю установщик: $asset"
    download "$url_gh" "$work/$asset" || die "не удалось скачать $url_gh"
    actual_size="$(file_size "$work/$asset")"
    if [ "$size" != "0" ] && [ "$actual_size" != "$size" ]; then
        die "размер скачанного файла ($actual_size) не совпал с релизом ($size)"
    fi
    sha="$($sha256_cmd "$work/$asset" | awk '{ print $1 }')"
    sha_lower="$(lower "$sha")"
    [ ${#sha_lower} = 64 ] || die "не посчитал SHA-256 (получилось '$sha')"
fi

if [ "$skip_download" = 0 ] && { [ "$URL_KIND" = "domain" ] || [ "$KEEP_ASSET" = 1 ]; }; then
    install -d "$WEB_DIR"
    if [ ! -f "$WEB_DIR/$asset" ] || ! cmp -s "$work/$asset" "$WEB_DIR/$asset"; then
        cp -f "$work/$asset" "$WEB_DIR/$asset"
        chmod 644 "$WEB_DIR/$asset"
        log "==> Установщик положен рядом с манифестом: $WEB_DIR/$asset"
    fi
fi

if [ "$SKIP_VERIFY" = 0 ] && [ "$skip_download" = 0 ]; then
    log '==> Проверяю, что файл уже доступен по ссылке'
    remote="$(remote_size "$url")" || die "файл по адресу $url недоступен — манифест не обновляю"
    [ "$remote" = "$actual_size" ] || die "по адресу $url размер $remote, а должно быть $actual_size — это другой файл"
    log "    доступен, размер совпадает ($remote Б)"
fi

if [ -z "$NOTES" ]; then
    NOTES="$(printf '%s' "$body" | tr '\n\r' '  ' | sed 's/  */ /g')"
    NOTES="$(truncate_text "$NOTES" 600)"
fi
if [ -z "$NOTES" ]; then
    warn "заметки пусты: ни --notes, ни описания релиза"
fi

M_VERSION="$version" \
M_URL="$url" \
M_SHA="$sha_lower" \
M_SIZE="$actual_size" \
M_NOTES="$NOTES" \
M_PUBLISHED="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
build_manifest > "$work/latest.json"

if [ "$DRY_RUN" = 1 ]; then
    log '==> --dry-run: манифест не записываю, был бы такой:'
    cat "$work/latest.json"
else
    install -d "$WEB_DIR"
    [ -f "$target" ] && cp -f "$target" "$target.prev"
    cp -f "$work/latest.json" "$WEB_DIR/.latest.json.new"
    chmod 644 "$WEB_DIR/.latest.json.new"
    mv -f "$WEB_DIR/.latest.json.new" "$target"
    log "==> Манифест записан: $target"
fi

if [ "$SKIP_VERIFY" = 0 ] && [ "$DRY_RUN" = 0 ]; then
    log '==> Проверка глазами клиента'
    if curl -fsS "$MANIFEST_URL" -o "$work/live.json" 2>/dev/null; then
        for field in version url sha256 size; do
            live_value="$(json_get "$work/live.json" "$field")"
            case "$field" in
                version) want="$version" ;;
                url) want="$url" ;;
                sha256) want="$sha_lower"; live_value="$(lower "$live_value")" ;;
                size) want="$actual_size" ;;
            esac
            if [ "$live_value" = "$want" ]; then
                log "    $field: совпадает"
            else
                warn "    $field: РАСХОДИТСЯ (опубликовано '$live_value', ожидалось '$want')"
            fi
        done
    else
        warn "манифест по адресу $MANIFEST_URL недоступен — проверьте раздачу каталога (Caddy, root, права)"
    fi
fi

log ''
log "Версия:   $version"
log "Ссылка:   $url"
log "SHA-256:  $sha_lower"
log "Размер:   $actual_size Б"
log "Манифест: $target"

