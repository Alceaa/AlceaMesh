package coordinator

type Store interface {
	PutDevice(Device) error
	GetDevice(id string) (Device, bool, error)
	GetDeviceBySecret(secret string) (Device, bool, error)
	GetDeviceByPublicKey(publicKey string) (Device, bool, error)
	ListDevices() ([]Device, error)
	DeleteDevice(id string) error

	PutToken(Token) error
	GetToken(value string) (Token, bool, error)
	ListTokens() ([]Token, error)
}
