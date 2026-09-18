module github.com/crs2007/callmqtt

go 1.27.0

require (
	github.com/eclipse/paho.golang v0.23.0
	github.com/gogpu/systray v0.3.0
	golang.org/x/sys v0.48.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/go-webgpu/goffi v0.6.3 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	golang.org/x/net v0.43.0 // indirect
)

replace github.com/gogpu/systray => ./third_party/systray
