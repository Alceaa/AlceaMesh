//go:build !windows

package wgengine

import (
	"errors"

	"golang.zx2c4.com/wireguard/tun"
)

func (t *Tunnel) configureInterface(tunDev tun.Device, mtu int) error {
	return errors.New("interface configuration is not implemented on this platform yet")
}

func (t *Tunnel) deconfigureInterface() error {
	return nil
}
