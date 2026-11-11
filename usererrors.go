package main

import (
	"errors"
	"strings"
	"unicode"
)

const (
	serviceUnavailable = "Служба AlceaMesh не отвечает. Перезапустите приложение или запустите службу «AlceaMesh» от имени администратора."

	fallbackService  = "Не удалось выполнить действие. Перезапустите приложение и повторите."
	fallbackNetwork  = "Сервер Alcea отклонил запрос. Попробуйте позже."
	fallbackPeer     = "Прямое соединение недоступно. Перезапустите приложение."
	fallbackTransfer = "Передача не удалась. Повторите попытку."
	fallbackSettings = "Не удалось сохранить настройки устройства."
	fallbackFile     = "Не удалось прочитать выбранный файл."
	fallbackDialog   = "Не удалось открыть диалог выбора файла."
	fallbackWindow   = "Окно приложения ещё не готово. Повторите попытку."
)

type userMessage struct {
	match string
	text  string
}

var userMessages = []userMessage{
	{"p2p: peer public key is required", "Устройство не выбрано."},
	{"p2p: signaling is not configured", "Связь с сетью недоступна. Перезапустите приложение."},
	{"p2p: session with ", "Подключение к этому устройству уже идёт."},
	{"p2p: no session with", "Соединение с устройством не установлено."},
	{"p2p: the direct path is not established yet", "Прямое соединение ещё не установлено."},
	{"p2p: send offer", "Не удалось согласовать адреса с устройством. Проверьте подключение к интернету."},
	{"p2p: bind udp socket", "Не удалось открыть сетевой порт. Разрешите приложению доступ в брандмауэре Windows."},
	{"p2p: no stun response", "Не удалось определить внешний адрес. Проверьте подключение к интернету."},
	{"p2p: agent is not running", "Прямые соединения недоступны. Перезапустите приложение."},
	{"p2p: agent closed", "Прямые соединения недоступны. Перезапустите приложение."},

	{"transfer: peer identity does not match", "Устройство не прошло проверку подлинности. Передача отменена."},
	{"transfer: peer is not in the Alcea network", "Устройство не в сети Alcea. Передача отклонена."},
	{"no recent network activity", "Устройство не отвечает. Проверьте, что VPN подключён у обоих."},
	{"handshake did not complete", "Не удалось установить защищённое соединение. Проверьте, что VPN подключён у обоих."},
	{"cannot assign requested address", "Адрес VPN недоступен. Подключите VPN и повторите."},
	{"transfer: checksum mismatch", "Файл принят с ошибкой. Отправьте его ещё раз."},
	{"transfer: connection lost", "Связь с устройством прервалась. Повторите передачу."},
	{"transfer: send data", "Связь с устройством прервалась. Повторите передачу."},
	{"transfer: read header", "Связь с устройством прервалась. Повторите передачу."},
	{"transfer: await confirmation", "Устройство не подтвердило получение. Повторите передачу."},
	{"transfer: await acknowledgement", "Устройство не подтвердило получение. Повторите передачу."},
	{"transfer: read reply", "Устройство не ответило. Повторите передачу."},
	{"transfer: decode reply", "Устройство ответило непонятно. Повторите передачу."},
	{"transfer: decode confirmation", "Устройство ответило непонятно. Повторите передачу."},
	{"transfer: decode header", "Устройство отправило повреждённые данные."},
	{"transfer: connect to peer", "Не удалось связаться с устройством. Проверьте, что оно в сети."},
	{"transfer: nothing to send", "В выбранной папке нет файлов для отправки."},
	{"update: checksum mismatch", "Файл обновления повреждён. Повторите позже."},
	{"update: manifest status", "Не удалось проверить обновления. Попробуйте позже."},
	{"update: fetch manifest", "Не удалось проверить обновления. Проверьте подключение к интернету."},
	{"update: download", "Не удалось скачать обновление. Проверьте подключение к интернету."},
	{"transfer: file name must not contain a path", "Устройство отправило файл с недопустимым именем."},
	{"transfer: invalid file name", "Устройство отправило файл с недопустимым именем."},
	{"transfer: negative file size", "Устройство отправило файл неверного размера."},
	{"transfer: peer requested an invalid offset", "Устройство отклонило докачку файла. Отправьте его заново."},
	{"transfer: sender did not provide a checksum", "Устройство отправило файл без контрольной суммы."},
	{"transfer: invalid local private key", "Ключ устройства не подходит. Импортируйте конфигурацию заново."},
	{"transfer: invalid peer public key", "Ключ удалённого устройства не подходит."},
	{"transfer: empty shared secret", "Ключ устройства не подходит. Импортируйте конфигурацию заново."},
	{"transfer: create certificate", "Не удалось подготовить защищённое соединение. Повторите попытку."},
	{"transfer: listen", "Не удалось принять входящее соединение. Повторите попытку."},

	{"coordinator returned", "Сервер Alcea отклонил запрос. Попробуйте позже."},
	{"decode response", "Сервер Alcea ответил непонятно. Попробуйте позже."},
	{"no such host", "Сервер Alcea недоступен. Проверьте подключение к интернету."},
	{"dial tcp", "Сервер Alcea недоступен. Проверьте подключение к интернету."},
	{"connectex", "Сервер Alcea недоступен. Проверьте подключение к интернету."},
	{"wsarecv", "Сервер Alcea недоступен. Проверьте подключение к интернету."},
	{"connection refused", "Сервер Alcea недоступен. Проверьте подключение к интернету."},
	{"network is unreachable", "Сервер Alcea недоступен. Проверьте подключение к интернету."},
	{"i/o timeout", "Сервер Alcea не ответил вовремя. Проверьте подключение к интернету."},
	{"context deadline exceeded", "Сервер Alcea не ответил вовремя. Проверьте подключение к интернету."},
	{"context canceled", "Действие отменено."},

	{"is already up", "VPN уже подключён."},
	{"interface configuration is not implemented", "VPN пока не поддерживается на этой системе."},
	{"no IPv4 default route found", "Не удалось найти сеть для VPN. Проверьте подключение к интернету."},
	{"pin endpoint routes", "Не удалось подготовить маршруты VPN. Проверьте подключение к интернету и повторите."},
	{"create adapter", "Не удалось включить VPN. Перезапустите службу «AlceaMesh» и повторите."},
	{"configure interface", "Не удалось включить VPN. Перезапустите службу «AlceaMesh» и повторите."},
	{"configure ip interface", "Не удалось включить VPN. Перезапустите службу «AlceaMesh» и повторите."},
	{"set ip addresses", "Не удалось включить VPN. Перезапустите службу «AlceaMesh» и повторите."},
	{"set routes", "Не удалось включить VPN. Перезапустите службу «AlceaMesh» и повторите."},
	{"set dns", "Не удалось включить VPN. Перезапустите службу «AlceaMesh» и повторите."},
	{"build uapi config", "Не удалось включить VPN. Перезапустите службу «AlceaMesh» и повторите."},
	{"apply config", "Не удалось включить VPN. Перезапустите службу «AlceaMesh» и повторите."},
	{"bring device up", "Не удалось включить VPN. Перезапустите службу «AlceaMesh» и повторите."},
}

func friendlyText(err error, fallback string) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	for _, rule := range userMessages {
		if strings.Contains(message, rule.match) {
			return rule.text
		}
	}
	if hasCyrillic(message) {
		return message
	}
	return fallback
}

func friendlyError(err error, fallback string) error {
	if err == nil {
		return nil
	}
	return errors.New(friendlyText(err, fallback))
}

func hasCyrillic(text string) bool {
	for _, symbol := range text {
		if unicode.Is(unicode.Cyrillic, symbol) {
			return true
		}
	}
	return false
}
