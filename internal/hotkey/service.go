package hotkey

import (
	"context"
	"sync"
)

type EventType string

const (
	Activated   EventType = "activated"
	Deactivated EventType = "deactivated"
)

type Event struct {
	Type EventType
}

type Service interface {
	Plan() string
	Events(context.Context) (<-chan Event, error)
}

type PlannedService struct {
	Description string
}

func (s PlannedService) Plan() string {
	return s.Description
}

func (s PlannedService) Events(ctx context.Context) (<-chan Event, error) {
	events := make(chan Event)
	go func() {
		<-ctx.Done()
		close(events)
	}()
	return events, nil
}

type ExternalTriggerService struct {
	description string
	events      chan Event

	mu        sync.Mutex
	active    bool
	closeOnce sync.Once
}

func NewExternalTriggerService(description string) *ExternalTriggerService {
	return &ExternalTriggerService{
		description: description,
		events:      make(chan Event, 16),
	}
}

func (s *ExternalTriggerService) Plan() string {
	return s.description
}

func (s *ExternalTriggerService) Events(ctx context.Context) (<-chan Event, error) {
	go func() {
		<-ctx.Done()
		s.closeOnce.Do(func() {
			close(s.events)
		})
	}()
	return s.events, nil
}

func (s *ExternalTriggerService) TriggerStart() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active {
		return false
	}

	s.active = true
	s.emit(Event{Type: Activated})
	return true
}

func (s *ExternalTriggerService) TriggerStop() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active {
		return false
	}

	s.active = false
	s.emit(Event{Type: Deactivated})
	return true
}

func (s *ExternalTriggerService) TriggerToggle() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.active = !s.active
	if s.active {
		s.emit(Event{Type: Activated})
	} else {
		s.emit(Event{Type: Deactivated})
	}
	return s.active
}

func (s *ExternalTriggerService) Active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

func (s *ExternalTriggerService) emit(event Event) {
	select {
	case s.events <- event:
	default:
	}
}
