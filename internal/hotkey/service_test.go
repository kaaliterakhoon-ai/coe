package hotkey

import "testing"

func TestExternalTriggerServiceToggle(t *testing.T) {
	t.Parallel()

	service := NewExternalTriggerService("external")

	if service.Active() {
		t.Fatal("expected service to start inactive")
	}

	if !service.TriggerToggle() {
		t.Fatal("expected toggle to activate service")
	}

	if !service.Active() {
		t.Fatal("expected service to be active")
	}

	if service.TriggerToggle() {
		t.Fatal("expected second toggle to deactivate service")
	}

	if service.Active() {
		t.Fatal("expected service to be inactive")
	}
}
