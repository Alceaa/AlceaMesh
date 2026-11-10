package main

import (
	"errors"
	"fmt"
	"testing"
)

func TestFriendlyText(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		fallback string
		want     string
	}{
		{"nil error", nil, fallbackService, ""},
		{"passthrough russian", errors.New("Импортируйте конфигурацию, чтобы войти в сеть."), fallbackService, "Импортируйте конфигурацию, чтобы войти в сеть."},
		{"p2p bind", errors.New("p2p: bind udp socket: listen udp4 :0: bind: Only one usage of each socket address"), fallbackPeer, "Не удалось открыть сетевой порт. Разрешите приложению доступ в брандмауэре Windows."},
		{"p2p no session", errors.New(`p2p: no session with "Ноутбук"`), fallbackPeer, "Соединение с устройством не установлено."},
		{"p2p not direct yet", errors.New("p2p: the direct path is not established yet"), fallbackPeer, "Прямое соединение ещё не установлено."},
		{"russian prefix wins", fmt.Errorf("Не удалось открыть P2P-сокет: %w", errors.New("p2p: bind udp socket: boom")), fallbackPeer, "Не удалось открыть сетевой порт. Разрешите приложению доступ в брандмауэре Windows."},
		{"transfer identity", errors.New("transfer: peer identity does not match the WireGuard key"), fallbackTransfer, "Устройство не прошло проверку подлинности. Передача отменена."},
		{"transfer checksum", errors.New("transfer: checksum mismatch (got aa, want bb)"), fallbackTransfer, "Файл принят с ошибкой. Отправьте его ещё раз."},
		{"transfer beats network rule", errors.New("transfer: send data: write udp 10.0.0.2:51001: wsasend: connection reset"), fallbackTransfer, "Связь с устройством прервалась. Повторите передачу."},
		{"transfer name", errors.New("transfer: file name must not contain a path"), fallbackTransfer, "Устройство отправило файл с недопустимым именем."},
		{"empty folder", errors.New("transfer: nothing to send"), fallbackTransfer, "В выбранной папке нет файлов для отправки."},
		{"network unreachable", errors.New(`Get "http://vps.alceacore.ru:7788/v1/presence": dial tcp 10.0.0.1:7788: connectex: A connection attempt failed`), fallbackNetwork, "Сервер Alcea недоступен. Проверьте подключение к интернету."},
		{"network dns", errors.New(`Get "http://vps.alceacore.ru:7788/v1/peers": dial tcp: lookup vps.alceacore.ru: no such host`), fallbackNetwork, "Сервер Alcea недоступен. Проверьте подключение к интернету."},
		{"coordinator status", errors.New("coordinator returned 500 Internal Server Error"), fallbackNetwork, "Сервер Alcea отклонил запрос. Попробуйте позже."},
		{"tunnel already up", errors.New(`tunnel "AlceaMesh" is already up`), fallbackService, "VPN уже подключён."},
		{"engine adapter", errors.New(`create adapter "AlceaMesh": wintun: dll not found`), fallbackService, "Не удалось включить VPN. Перезапустите службу «AlceaMesh» и повторите."},
		{"engine dns", errors.New("set dns: access is denied"), fallbackService, "Не удалось включить VPN. Перезапустите службу «AlceaMesh» и повторите."},
		{"unknown english uses fallback", errors.New(`open C:\Users\alcea\settings.json: Access is denied.`), fallbackSettings, fallbackSettings},
		{"unknown english transfer", errors.New("unexpected EOF"), fallbackTransfer, fallbackTransfer},
	}

	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if got := friendlyText(item.err, item.fallback); got != item.want {
				t.Fatalf("friendlyText() = %q, want %q", got, item.want)
			}
		})
	}
}

func TestFriendlyError(t *testing.T) {
	if err := friendlyError(nil, fallbackPeer); err != nil {
		t.Fatalf("friendlyError(nil) = %v, want nil", err)
	}
	err := friendlyError(errors.New("p2p: agent closed"), fallbackPeer)
	if got := err.Error(); got != "Прямые соединения недоступны. Перезапустите приложение." {
		t.Fatalf("friendlyError() = %q", got)
	}
}

func TestHasCyrillic(t *testing.T) {
	if !hasCyrillic("Служба AlceaMesh") {
		t.Fatal("hasCyrillic() = false for russian text")
	}
	if hasCyrillic("p2p: bind udp socket") {
		t.Fatal("hasCyrillic() = true for english text")
	}
	if hasCyrillic("") {
		t.Fatal("hasCyrillic() = true for empty text")
	}
}
