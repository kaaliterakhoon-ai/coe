package portal

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

const (
	DesktopBusName            = "org.freedesktop.portal.Desktop"
	DesktopObjectPath         = "/org/freedesktop/portal/desktop"
	GlobalShortcutsInterface  = "org.freedesktop.portal.GlobalShortcuts"
	RemoteDesktopInterface    = "org.freedesktop.portal.RemoteDesktop"
	ClipboardInterface        = "org.freedesktop.portal.Clipboard"
	IntrospectableMethod      = "org.freedesktop.DBus.Introspectable.Introspect"
	globalShortcutsVersionKey = GlobalShortcutsInterface + ".version"
	remoteDesktopVersionKey   = RemoteDesktopInterface + ".version"
	clipboardVersionKey       = ClipboardInterface + ".version"
)

type InterfaceStatus struct {
	Available bool
	Version   uint32
}

type Interfaces struct {
	GlobalShortcuts InterfaceStatus
	RemoteDesktop   InterfaceStatus
	Clipboard       InterfaceStatus
}

type Client struct {
	conn *dbus.Conn
	obj  dbus.BusObject
}

func ConnectSession() (*Client, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}

	return NewClient(conn), nil
}

func NewClient(conn *dbus.Conn) *Client {
	return &Client{
		conn: conn,
		obj:  conn.Object(DesktopBusName, dbus.ObjectPath(DesktopObjectPath)),
	}
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) Probe(ctx context.Context) (Interfaces, error) {
	node, err := c.Introspect(ctx)
	if err != nil {
		return Interfaces{}, err
	}

	status := ClassifyNode(node)
	var versionErr error

	if status.GlobalShortcuts.Available {
		status.GlobalShortcuts.Version, err = c.interfaceVersion(globalShortcutsVersionKey)
		if err != nil {
			versionErr = joinProbeError(versionErr, fmt.Errorf("read GlobalShortcuts version: %w", err))
		}
	}

	if status.RemoteDesktop.Available {
		status.RemoteDesktop.Version, err = c.interfaceVersion(remoteDesktopVersionKey)
		if err != nil {
			versionErr = joinProbeError(versionErr, fmt.Errorf("read RemoteDesktop version: %w", err))
		}
	}

	if status.Clipboard.Available {
		status.Clipboard.Version, err = c.interfaceVersion(clipboardVersionKey)
		if err != nil {
			versionErr = joinProbeError(versionErr, fmt.Errorf("read Clipboard version: %w", err))
		}
	}

	return status, versionErr
}

func (c *Client) Introspect(ctx context.Context) (*introspect.Node, error) {
	var xmlData string
	if err := c.obj.CallWithContext(ctx, IntrospectableMethod, 0).Store(&xmlData); err != nil {
		return nil, err
	}

	var node introspect.Node
	if err := xml.NewDecoder(strings.NewReader(xmlData)).Decode(&node); err != nil {
		return nil, err
	}

	if node.Name == "" {
		node.Name = DesktopObjectPath
	}

	return &node, nil
}

func (c *Client) interfaceVersion(property string) (uint32, error) {
	var version uint32
	if err := c.obj.StoreProperty(property, &version); err != nil {
		return 0, err
	}
	return version, nil
}

func ClassifyNode(node *introspect.Node) Interfaces {
	result := Interfaces{}
	if node == nil {
		return result
	}

	for _, iface := range node.Interfaces {
		switch iface.Name {
		case GlobalShortcutsInterface:
			result.GlobalShortcuts.Available = true
		case RemoteDesktopInterface:
			result.RemoteDesktop.Available = true
		case ClipboardInterface:
			result.Clipboard.Available = true
		}
	}

	return result
}

func joinProbeError(existing error, next error) error {
	if existing == nil {
		return next
	}
	return errors.Join(existing, next)
}
