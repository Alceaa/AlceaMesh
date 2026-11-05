# AlceaMesh

Десктопное приложение для Windows с двумя функциями:

1. **P2P-передача файлов** между устройствами.
2. **Интерфейс для VPN** (WireGuard), аналог официального клиента WireGuard.

---

## Движок WireGuard

Приложение **не вызывает** `wg.exe` и **не зависит** от официального клиента WireGuard.
Движок `wireguard-go` встроен прямо в бинарник как библиотека:

```
   .conf  ──►  wgconf.Parse  ──►  Config (структура)
                                   │
                                   ├─ ToUAPI()  ──►  device.IpcSet()   (приватный ключ, пиры)
                                   │
                                   └─ Address/AllowedIPs/DNS ──►  winipcfg  ──►  адаптер Windows

   wireguard-go:  tun.CreateTUN("AlceaMesh", mtu)  ──►  wintun.dll (TUN-драйвер)
                  device.NewDevice(tun, udpBind, logger)
```

- **`wintun.dll`** — драйвер TUN для Windows (создаёт сетевой адаптер). Загружается из
  каталога рядом с `.exe`, поэтому DLL кладётся в `bin/` вместе с бинарником.
- **`wireguard-go`** — userspace-реализация протокола WireGuard. Шифрование, хендшейки,
  передача UDP-пакетов — всё внутри процесса.
- **`winipcfg`** — обёртка над Windows IP Helper API: назначение IP, маршрутов, DNS.

### Сохранение маршрута до endpoint

Если в конфиге `AllowedIPs = 0.0.0.0/0` (полный туннель), то весь трафик уходит в туннель —
**включая** сами UDP-пакеты WireGuard к серверу, что разорвало бы соединение. Поэтому движок
перед добавлением маршрутов туннеля добавляет отдельный `/32`-маршрут к IP-адресу endpoint
через физический шлюз. Это делается автоматически (`preserveEndpointRoutes`).

---

## Сервис и IPC

- **`alceameshd`** — привилегированный процесс. Держит все туннели, слушает IPC-сокет.
  - `alceameshd run` — запустить в foreground (для тестов, от админа);
  - `alceameshd install` / `uninstall` — зарегистрировать/удалить как службу Windows.
- **`alceamesh-cli`** — непривилегированный клиент: командует сервисом через IPC.
- **IPC-протокол** — построчный JSON поверх unix-сокета:
  `{"id":1,"method":"tunnel.up","params":{...}}` → `{"id":1,"result":{...}}`.

Путь сокета: `%ProgramData%\AlceaMesh\alceamesh.sock` (Windows) / `/run/alceamesh` (Linux).
При создании каталога сокета на Windows выставляется DACL, чтобы клиенты под обычным
пользователем могли подключаться к сервису, запущенному как SYSTEM.

Методы: `ping`, `version`, `status`, `tunnel.up`, `tunnel.down`.

---

## Координатор (control plane)

**`alceacoord`** — отдельный сервис для VPS. Это «control plane»: он **не** передаёт и
**не** хранит файлы, а ведёт только метаданные:

1. **Реестр устройств** — кто есть в сети (имя, публичный ключ, IP в меше, платформа).
2. **Enrollment** — регистрация нового устройства по **токену** (без аккаунтов).
3. **Presence** — онлайн/оффлайн по heartbeat с TTL («молчит дольше N — оффлайн»).

### Как это устроено

```
   устройство                 координатор (alceacoord)
   ──────────                 ────────────────────────
   enroll(token,name,…) ────►  Registry.Enroll ──► Store (SQLite)  выдаёт mesh IP + secret
   heartbeat (Bearer secret)─► Registry.Heartbeat   обновляет last_seen
   devices  (Bearer secret) ─► Registry.Devices     список + online/offline
```

- **Транспорт** — HTTP/JSON на стандартной библиотеке Go (`net/http`), без сторонних фреймворков.
- **Хранилище** — SQLite (чистый Go, `modernc.org/sqlite`) за интерфейсом `Store`;
  в тестах используется in-memory реализация.
- **Присутствие** вычисляется на лету: устройство «online», если `now − last_seen < presence-ttl`.
- **Идентичность без аккаунтов**: при enrollment устройство получает публичный `id` и
  секрет `secret` (32 байта). Секрет — это его «пароль» для heartbeat и списка устройств;
  в списке устройств он никогда не показывается.
- **Токены enrollment** одноразовые, с опциональным TTL и опционально привязанным mesh IP.
- **mesh IP** при отсутствии привязки выдаётся автоматически из пула (`--pool`): пропускаются
  сетевой, широковещательный адреса и шлюз (первый хост).

### Запуск на VPS

```powershell
$env:ALCEAMESH_ADMIN_KEY = "<длинный случайный ключ>"
.\bin\alceacoord.exe run --listen :7788 --db alceacoord.db --pool 10.0.0.0/24
```

Админ-ключ (`--admin-key` / env `ALCEAMESH_ADMIN_KEY`) защищает управление токенами и удаление
устройств. Сервис слушает plain HTTP — на VPS его принято ставить за TLS-обратный прокси.

Создать токен можно и локально на сервере, без обращения к API:

```powershell
.\bin\alceacoord.exe token --db alceacoord.db --label "ноутбук Алисы" --ttl-hours 24
```

### API

| Метод | Путь | Авторизация | Назначение |
|-------|------|-------------|------------|
| `GET`  | `/v1/ping` | — | проверка живости |
| `GET`  | `/v1/version` | — | версия координатора |
| `POST` | `/v1/enroll` | токен в теле | регистрация устройства |
| `POST` | `/v1/heartbeat` | `Bearer <secret>` | отметка присутствия |
| `GET`  | `/v1/devices` | `Bearer <secret>` или админ-ключ | список устройств |
| `POST` | `/v1/tokens` | админ-ключ | создать токен |
| `GET`  | `/v1/tokens` | админ-ключ | список токенов |
| `DELETE` | `/v1/devices/{id}` | админ-ключ | удалить устройство |

### CLI

```powershell
$env:ALCEAMESH_COORDINATOR = "http://vps.alceacore.ru:7788"
.\bin\alceamesh-cli.exe coord ping
.\bin\alceamesh-cli.exe coord devices --admin-key <ключ>        # таблица: имя, mesh ip, online/offline
$t = .\bin\alceamesh-cli.exe coord token --admin-key <ключ> --label "телефон"
.\bin\alceamesh-cli.exe coord enroll --token $t --name "Телефон" --platform android
```

---

## Сборка

```powershell
.\scripts\build.ps1
```

> Если PowerShell блокирует запуск скриптов (execution policy), выполните:
> `powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1`

Результат — четыре файла в `bin\`: `alceameshd.exe` (сервис), `alceamesh-cli.exe` (клиент),
`alceacoord.exe` (координатор для VPS) и `wintun.dll` (драйвер TUN; должен лежать рядом с сервисом).

Либо вручную:

```powershell
go build -o bin\alceameshd.exe    .\cmd\alceameshd
go build -o bin\alceamesh-cli.exe .\cmd\alceamesh-cli
go build -o bin\alceacoord.exe    .\cmd\alceacoord
Copy-Item third_party\wintun.dll bin\
```

---

## Использование

Запустите сервис (в отдельном окне, **от имени администратора**):

```powershell
.\bin\alceameshd.exe run
```

В другом окне (обычные права) управляйте туннелями:

```powershell
.\bin\alceamesh-cli.exe ping                       # проверить, что сервис отвечает
.\bin\alceamesh-cli.exe status                     # список туннелей
.\bin\alceamesh-cli.exe up examples\sample.conf    # поднять туннель
.\bin\alceamesh-cli.exe down sample                # опустить туннель
```

Офлайн-команды (сервис не нужен):

```powershell
.\bin\alceamesh-cli.exe show examples\sample.conf  # разобрать и показать конфиг
.\bin\alceamesh-cli.exe uapi examples\sample.conf  # UAPI-нагрузка для движка
```

Регистрация как службы Windows (от админа, автозапуск):

```powershell
.\bin\alceameshd.exe install
Start-Service AlceaMesh
```

> **Создание сетевого адаптера требует прав администратора.** Именно поэтому движок живёт
> в `alceameshd`, а CLI работает без админских прав.

---

## Структура репозитория

```
cmd/alceameshd/           привилегированный сервис (движок + IPC)
cmd/alceamesh-cli/        CLI-клиент сервиса
cmd/alceacoord/           координатор для VPS (control plane)
internal/ipc/             IPC: JSON-протокол, сервер, клиент, путь сокета
internal/daemon/          логика сервиса: управление туннелями
internal/client/          типизированный клиент сервиса
internal/coordinator/     ядро координатора: реестр, enrollment, presence (+ тесты)
internal/coordinator/api/ HTTP/JSON API координатора (+ тесты)
internal/coordinator/sqlitestore/  хранилище координатора на SQLite (+ тесты)
internal/coordclient/     HTTP-клиент координатора
internal/wgconf/          парсер и сериализатор WireGuard .conf (+ тесты)
internal/wgengine/        движок туннеля (wireguard-go + wintun + настройка Windows)
third_party/wintun.dll    подписанный драйвер TUN (см. wintun-LICENSE.txt)
examples/sample.conf      пример конфигурации
scripts/build.ps1         сборка
```

---

## Тесты

```powershell
go test ./...
```

- `internal/wgconf` — парсер конфигов: полный/минимальный `.conf`, round-trip, UAPI
  (base64 → hex), обработка ошибок.
- `internal/wgengine` — построение маршрутов: next-hop по семейству адреса, маскирование,
  дедупликация, определение default-route, `Table = off`.
- `internal/ipc` — запрос/ответ, параметры, ошибка хендлера, неизвестный метод, серия вызовов.
- `internal/coordinator` — реестр/enrollment/presence: одноразовые и просроченные токены,
  выдача mesh IP (пропуск шлюза и занятых), исчерпание пула, presence по TTL, heartbeat.
- `internal/coordinator/api` — HTTP-слой: ping/version, поток enroll→heartbeat→devices,
  проверка авторизации (401), удаление устройства.
- `internal/coordinator/sqlitestore` — CRUD и персистентность SQLite, работа реестра с этим стором.
- `internal/coordclient` — round-trip клиента координатора через реальный HTTP-сервер.

---

## Лицензии сторонних компонентов

- `wireguard-go`, `wintun` (Go-биндинги), `quic-go`, `winipcfg` — MIT.
- `wintun.dll` (prebuilt) — распространяется по более мягкой лицензии, см.
  `third_party/wintun-LICENSE.txt`. DLL распространяется **как есть**, без переименования.
