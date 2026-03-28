package dbus

import (
	"context"
	"fmt"

	godbus "github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

const (
	DictationServiceName = "com.mistermorph.Coe"
	DictationObjectPath  = godbus.ObjectPath("/com/mistermorph/Coe")
	DictationInterface   = "com.mistermorph.Coe.Dictation1"
)

type Status struct {
	State     string
	SessionID string
	Detail    string
}

type Handler interface {
	Toggle(context.Context) error
	Start(context.Context) error
	Stop(context.Context) error
	Status(context.Context) Status
	TriggerKey(context.Context) string
}

type Service struct {
	conn *godbus.Conn
}

func ConnectSession(handler Handler) (*Service, error) {
	conn, err := godbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}

	reply, err := conn.RequestName(DictationServiceName, godbus.NameFlagDoNotQueue)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if reply != godbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		return nil, fmt.Errorf("D-Bus name %s is already owned", DictationServiceName)
	}

	object := &dictationObject{handler: handler}
	if err := conn.Export(object, DictationObjectPath, DictationInterface); err != nil {
		_, _ = conn.ReleaseName(DictationServiceName)
		conn.Close()
		return nil, err
	}

	node := &introspect.Node{
		Name: string(DictationObjectPath),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			{
				Name: DictationInterface,
				Methods: []introspect.Method{
					{Name: "Toggle"},
					{Name: "Start"},
					{Name: "Stop"},
					{
						Name: "Status",
						Args: []introspect.Arg{
							{Name: "state", Type: "s", Direction: "out"},
							{Name: "session_id", Type: "s", Direction: "out"},
							{Name: "detail", Type: "s", Direction: "out"},
						},
					},
					{
						Name: "TriggerKey",
						Args: []introspect.Arg{
							{Name: "trigger_key", Type: "s", Direction: "out"},
						},
					},
				},
				Signals: []introspect.Signal{
					{
						Name: "StateChanged",
						Args: []introspect.Arg{
							{Name: "state", Type: "s"},
							{Name: "session_id", Type: "s"},
							{Name: "detail", Type: "s"},
						},
					},
					{
						Name: "ResultReady",
						Args: []introspect.Arg{
							{Name: "session_id", Type: "s"},
							{Name: "text", Type: "s"},
						},
					},
					{
						Name: "ErrorRaised",
						Args: []introspect.Arg{
							{Name: "session_id", Type: "s"},
							{Name: "message", Type: "s"},
						},
					},
				},
			},
		},
	}
	conn.Export(introspect.NewIntrospectable(node), DictationObjectPath, "org.freedesktop.DBus.Introspectable")

	return &Service{conn: conn}, nil
}

func (s *Service) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	_, _ = s.conn.ReleaseName(DictationServiceName)
	return s.conn.Close()
}

func (s *Service) EmitStateChanged(status Status) error {
	if s == nil || s.conn == nil {
		return nil
	}
	return s.conn.Emit(DictationObjectPath, DictationInterface+".StateChanged", status.State, status.SessionID, status.Detail)
}

func (s *Service) EmitResultReady(sessionID, text string) error {
	if s == nil || s.conn == nil {
		return nil
	}
	return s.conn.Emit(DictationObjectPath, DictationInterface+".ResultReady", sessionID, text)
}

func (s *Service) EmitError(sessionID, message string) error {
	if s == nil || s.conn == nil {
		return nil
	}
	return s.conn.Emit(DictationObjectPath, DictationInterface+".ErrorRaised", sessionID, message)
}

type dictationObject struct {
	handler Handler
}

func (o *dictationObject) Toggle() *godbus.Error {
	if err := o.handler.Toggle(context.Background()); err != nil {
		return godbus.MakeFailedError(err)
	}
	return nil
}

func (o *dictationObject) Start() *godbus.Error {
	if err := o.handler.Start(context.Background()); err != nil {
		return godbus.MakeFailedError(err)
	}
	return nil
}

func (o *dictationObject) Stop() *godbus.Error {
	if err := o.handler.Stop(context.Background()); err != nil {
		return godbus.MakeFailedError(err)
	}
	return nil
}

func (o *dictationObject) Status() (string, string, string, *godbus.Error) {
	status := o.handler.Status(context.Background())
	return status.State, status.SessionID, status.Detail, nil
}

func (o *dictationObject) TriggerKey() (string, *godbus.Error) {
	return o.handler.TriggerKey(context.Background()), nil
}
