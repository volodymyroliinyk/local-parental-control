package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
	"github.com/volodymyroliinyk/local-parental-control/internal/api"
	"github.com/volodymyroliinyk/local-parental-control/internal/daemon"
	"github.com/volodymyroliinyk/local-parental-control/internal/indicator"
)

const itemInterface = "org.kde.StatusNotifierItem"

var version = "development"

type tooltip struct {
	IconName string
	Pixmaps  []iconPixmap
	Title    string
	Text     string
}

type iconPixmap struct {
	Width, Height int32
	Data          []byte
}

type item struct{}

func (*item) ContextMenu(int32, int32) *dbus.Error       { return nil }
func (*item) Activate(int32, int32) *dbus.Error          { return nil }
func (*item) SecondaryActivate(int32, int32) *dbus.Error { return nil }
func (*item) Scroll(int32, string) *dbus.Error           { return nil }

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(version)
		return
	}
	status, date, err := awaitStatus()
	if err != nil {
		return
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return
	}
	defer conn.Close()
	name := fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid())
	if reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		return
	}
	path := dbus.ObjectPath("/StatusNotifierItem")
	properties, err := prop.Export(conn, path, propertiesFor(status, date))
	if err != nil || conn.Export(&item{}, path, itemInterface) != nil {
		return
	}
	register(conn, name)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		status, date, err = indicator.Read(daemon.DefaultStatusSocketPath)
		if errors.Is(err, indicator.ErrNotConfigured) {
			return
		}
		if err != nil {
			continue
		}
		properties.SetMust(itemInterface, "Status", statusState(status))
		properties.SetMust(itemInterface, "ToolTip", tooltipFor(status, date))
		properties.SetMust(itemInterface, "XAyatanaLabel", indicator.Label(status))
		register(conn, name)
	}
}

func awaitStatus() (*api.UserStatus, string, error) {
	for {
		status, date, err := indicator.Read(daemon.DefaultStatusSocketPath)
		if err == nil || errors.Is(err, indicator.ErrNotConfigured) {
			return status, date, err
		}
		time.Sleep(5 * time.Second)
	}
}

func propertiesFor(status *api.UserStatus, date string) prop.Map {
	constant := func(value any) *prop.Prop { return &prop.Prop{Value: value, Emit: prop.EmitConst} }
	changing := func(value any) *prop.Prop { return &prop.Prop{Value: value, Emit: prop.EmitTrue} }
	return prop.Map{itemInterface: {
		"Category":              constant("SystemServices"),
		"Id":                    constant("local-parental-control"),
		"Title":                 constant("Local Parental Control"),
		"Status":                changing(statusState(status)),
		"WindowId":              constant(uint32(0)),
		"IconName":              constant("preferences-system-time-symbolic"),
		"IconPixmap":            constant([]iconPixmap{}),
		"OverlayIconName":       constant(""),
		"OverlayIconPixmap":     constant([]iconPixmap{}),
		"AttentionIconName":     constant(""),
		"AttentionIconPixmap":   constant([]iconPixmap{}),
		"AttentionMovieName":    constant(""),
		"ToolTip":               changing(tooltipFor(status, date)),
		"ItemIsMenu":            constant(false),
		"Menu":                  constant(dbus.ObjectPath("/NO_DBUSMENU")),
		"XAyatanaLabel":         changing(indicator.Label(status)),
		"XAyatanaLabelGuide":    constant("Paused"),
		"XAyatanaOrderingIndex": constant(uint32(0)),
	}}
}

func statusState(status *api.UserStatus) string {
	// Keep the icon visually calm even while access is paused. The label and
	// tooltip carry the state without triggering an attention animation.
	_ = status
	return "Active"
}

func tooltipFor(status *api.UserStatus, date string) tooltip {
	return tooltip{IconName: "preferences-system-time-symbolic", Pixmaps: []iconPixmap{}, Title: "Time limits", Text: indicator.Tooltip(status, date)}
}

func register(conn *dbus.Conn, busName string) {
	watcher := conn.Object("org.kde.StatusNotifierWatcher", dbus.ObjectPath("/StatusNotifierWatcher"))
	_ = watcher.Call("org.kde.StatusNotifierWatcher.RegisterStatusNotifierItem", 0, busName).Err
}
