//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"alceacore.ru/alceamesh/internal/daemon"
	"alceacore.ru/alceamesh/internal/ipc"
)

const serviceName = "AlceaMesh"

func isWindowsService() bool {
	ok, err := svc.IsWindowsService()
	return err == nil && ok
}

func runAsService() error {
	return svc.Run(serviceName, &serviceHandler{})
}

type serviceHandler struct{}

func (h *serviceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}

	listener, err := ipc.Listen()
	if err != nil {
		return true, 1
	}

	d := daemon.New(version)
	server := ipc.NewServer()
	d.Register(server)

	done := make(chan struct{})
	go func() {
		server.Serve(listener)
		close(done)
	}()

	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case req := <-requests:
			switch req.Cmd {
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				listener.Close()
				<-done
				d.Shutdown()
				return false, 0
			}
		case <-done:
			d.Shutdown()
			return true, 1
		}
	}
}

func cmdInstall(_ []string) error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	if s, err := m.OpenService(serviceName); err == nil {
		s.Close()
		return fmt.Errorf("service %q is already installed", serviceName)
	}

	s, err := m.CreateService(serviceName, exePath, mgr.Config{
		DisplayName: "AlceaMesh Service",
		Description: "Owns the AlceaMesh WireGuard tunnels and the local IPC endpoint.",
		StartType:   mgr.StartAutomatic,
	})
	if err != nil {
		return err
	}
	defer s.Close()

	fmt.Printf("service %q installed\n", serviceName)
	return nil
}

func cmdUninstall(_ []string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %q is not installed: %w", serviceName, err)
	}
	defer s.Close()

	if err := s.Delete(); err != nil {
		return err
	}
	fmt.Printf("service %q removed\n", serviceName)
	return nil
}
