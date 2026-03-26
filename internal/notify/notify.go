package notify

import (
	"context"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	busName       = "org.freedesktop.Notifications"
	objectPath    = "/org/freedesktop/Notifications"
	interfaceName = "org.freedesktop.Notifications"
	notifyMethod  = interfaceName + ".Notify"
)

const (
	UrgencyLow byte = iota
	UrgencyNormal
	UrgencyCritical
)

type Service interface {
	Summary() string
	Send(context.Context, Message) error
	Close() error
}

type Message struct {
	Title   string
	Body    string
	Urgency byte
	Timeout time.Duration
}

type Disabled struct{}

func (Disabled) Summary() string {
	return "disabled"
}

func (Disabled) Send(context.Context, Message) error {
	return nil
}

func (Disabled) Close() error {
	return nil
}

type Desktop struct {
	conn    *dbus.Conn
	obj     dbus.BusObject
	appName string
}

func ConnectSession(appName string) (*Desktop, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}

	return &Desktop{
		conn:    conn,
		obj:     conn.Object(busName, dbus.ObjectPath(objectPath)),
		appName: appName,
	}, nil
}

func (d *Desktop) Summary() string {
	return "system (org.freedesktop.Notifications)"
}

func (d *Desktop) Send(ctx context.Context, msg Message) error {
	if d == nil || d.obj == nil || msg.Title == "" {
		return nil
	}

	timeout := int32(5000)
	if msg.Timeout > 0 {
		timeout = int32(msg.Timeout / time.Millisecond)
	}

	hints := map[string]dbus.Variant{
		"urgency": dbus.MakeVariant(msg.Urgency),
	}

	return d.obj.CallWithContext(
		ctx,
		notifyMethod,
		0,
		d.appName,
		uint32(0),
		"",
		msg.Title,
		msg.Body,
		[]string{},
		hints,
		timeout,
	).Err
}

func (d *Desktop) Close() error {
	if d == nil || d.conn == nil {
		return nil
	}
	return d.conn.Close()
}
