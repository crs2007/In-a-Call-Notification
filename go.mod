module github.com/crs2007/callmqtt

go 1.27.0

require (
	github.com/eclipse/paho.golang v0.23.0
	github.com/gogpu/systray v0.3.0
	github.com/shirou/gopsutil/v4 v4.26.8
	golang.org/x/sys v0.48.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/ebitengine/purego v0.10.2 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/go-webgpu/goffi v0.6.3 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/tklauser/go-sysconf v0.3.16 // indirect
	github.com/tklauser/numcpus v0.11.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	golang.org/x/net v0.43.0 // indirect
)

replace github.com/gogpu/systray => ./third_party/systray
