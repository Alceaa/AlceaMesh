# AlceaMesh

Десктопное приложение для Windows с двумя функциями:

1. **P2P-передача файлов** между устройствами.
2. **Интерфейс для VPN** (WireGuard), аналог официального клиента WireGuard.

---

## Как это работает (M0)

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

## Сборка

```powershell
.\scripts\build.ps1
```

> Если PowerShell блокирует запуск скриптов (execution policy), выполните:
> `powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1`

Результат — `bin\alceamesh-cli.exe` и `bin\wintun.dll` рядом.

Либо вручную:

```powershell
go build -o bin\alceamesh-cli.exe .\cmd\alceamesh-cli
Copy-Item third_party\wintun.dll bin\
```

---

## Использование CLI

```powershell
# версия
.\bin\alceamesh-cli.exe version

# разобрать и показать нормализованный конфиг
.\bin\alceamesh-cli.exe show examples\sample.conf

# показать полезную нагрузку UAPI, которая уходит в движок
.\bin\alceamesh-cli.exe uapi examples\sample.conf

# поднять туннель (ТРЕБУЕТСЯ ЗАПУСК ОТ ИМЕНИ АДМИНИСТРАТОРА)
.\bin\alceamesh-cli.exe up --verbose examples\sample.conf
```

Команда `up` работает в foreground и держит туннель, пока не нажмёте `Ctrl+C`.

> **Создание сетевого адаптера требует прав администратора.** Без них `wintun` вернёт
> `Access is denied`. Запускайте терминал «от имени администратора».

---

## Структура репозитория

```
cmd/alceamesh-cli/        CLI-утилита (точка входа для M0)
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

Покрыт парсер конфигов: разбор полного и минимального `.conf`, round-trip, генерация UAPI
(включая перевод ключей base64 → hex), обработка ошибок.

---

## Лицензии сторонних компонентов

- `wireguard-go`, `wintun` (Go-биндинги), `quic-go`, `winipcfg` — MIT.
- `wintun.dll` (prebuilt) — распространяется по более мягкой лицензии, см.
  `third_party/wintun-LICENSE.txt`. DLL распространяется **как есть**, без переименования.
