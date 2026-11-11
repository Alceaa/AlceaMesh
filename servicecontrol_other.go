//go:build !windows

package main

import "errors"

func runHidden(string, ...string) (string, error) {
	return "", errors.New("управление службой поддерживается только в Windows")
}

func runElevated(string, ...string) error {
	return errors.New("управление службой поддерживается только в Windows")
}

func serviceStart() (string, error) {
	return "", errors.New("управление службой поддерживается только в Windows")
}

func serviceQuery() (string, error) {
	return "", errors.New("управление службой поддерживается только в Windows")
}
