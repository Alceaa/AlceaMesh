package main

import (
	"testing"

	"alceacore.ru/alceamesh/internal/coordinator"
)

func TestVisibleDevice(t *testing.T) {
	cases := []struct {
		name string
		peer coordinator.DeviceView
		self bool
		want bool
	}{
		{
			name: "устройство принимает файлы и на связи",
			peer: coordinator.DeviceView{Available: true, Online: true},
			want: true,
		},
		{
			name: "устройство не принимает файлы",
			peer: coordinator.DeviceView{Available: false, Online: true},
			want: false,
		},
		{
			name: "устройство офлайн",
			peer: coordinator.DeviceView{Available: true, Online: false},
			want: false,
		},
		{
			name: "устройство без VPN",
			peer: coordinator.DeviceView{Available: false, Online: false},
			want: false,
		},
		{
			name: "своё устройство видно всегда",
			peer: coordinator.DeviceView{Available: false, Online: false},
			self: true,
			want: true,
		},
	}

	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if got := visibleDevice(item.peer, item.self); got != item.want {
				t.Errorf("visibleDevice = %v, want %v", got, item.want)
			}
		})
	}
}
