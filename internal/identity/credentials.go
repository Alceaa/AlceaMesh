package identity

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

type Credentials struct {
	DeviceID string
	Key      *Key
}

func (c Credentials) SignRequest(request *http.Request, body []byte, now time.Time) error {
	if c.Key == nil {
		return errors.New("identity: no signing key")
	}
	if c.DeviceID == "" {
		return errors.New("identity: no device id")
	}
	return c.Key.SignRequest(request, body, c.DeviceID, now)
}

func (c Credentials) PublicKey() string {
	if c.Key == nil {
		return ""
	}
	return c.Key.PublicKey()
}

type Reason string

const (
	ReasonUnknownDevice     Reason = "unknown_device"
	ReasonRevoked           Reason = "revoked"
	ReasonStale             Reason = "stale"
	ReasonReplay            Reason = "replay"
	ReasonBadSignature      Reason = "bad_signature"
	ReasonSignatureRequired Reason = "signature_required"
)

type RejectedError struct {
	Status int
	Reason Reason
	Detail string
}

func (e *RejectedError) Error() string {
	if e.Reason == "" {
		return e.Detail
	}
	return string(e.Reason) + ": " + e.Detail
}

func ParseRejection(status int, body []byte) *RejectedError {
	var payload struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	if payload.Reason == "" && payload.Error == "" {
		return nil
	}
	return &RejectedError{
		Status: status,
		Reason: Reason(payload.Reason),
		Detail: payload.Error,
	}
}

func (e *RejectedError) Matches(reason Reason) bool {
	return e != nil && e.Reason == reason
}

func (r Reason) HumanText() string {
	switch r {
	case ReasonUnknownDevice:
		return "Устройство не зарегистрировано в сети Alcea. Зарегистрируйтесь заново."
	case ReasonRevoked:
		return "Устройство отозвано владельцем сети. Нужна повторная регистрация."
	case ReasonStale:
		return "Проверьте время на компьютере: оно расходится с сервером больше чем на две минуты."
	case ReasonReplay:
		return "Запрос уже был отправлен. Повторите действие."
	case ReasonBadSignature:
		return "Не удалось подтвердить подлинность устройства. Если это повторяется, зарегистрируйтесь заново."
	case ReasonSignatureRequired:
		return "Сеть Alcea требует подписанных запросов. Обновите приложение."
	default:
		return ""
	}
}
