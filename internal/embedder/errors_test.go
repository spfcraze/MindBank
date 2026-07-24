package embedder

import (
	"errors"
	"testing"
)

func TestNewBusyError(t *testing.T) {
	e := NewBusyError("semaphore full")
	if e.Type != ErrBusy {
		t.Errorf("expected type %q, got %q", ErrBusy, e.Type)
	}
	if e.Message != "semaphore full" {
		t.Errorf("expected message %q, got %q", "semaphore full", e.Message)
	}
	if e.Cause != nil {
		t.Error("expected nil cause")
	}
	want := "[BUSY] semaphore full"
	if e.Error() != want {
		t.Errorf("expected error %q, got %q", want, e.Error())
	}
}

func TestNewUnavailableError(t *testing.T) {
	cause := errors.New("connection refused")
	e := NewUnavailableError("ollama down", cause)
	if e.Type != ErrUnavailable {
		t.Errorf("expected type %q, got %q", ErrUnavailable, e.Type)
	}
	if !errors.Is(e, cause) {
		t.Error("expected errors.Is to match cause")
	}
	want := "[UNAVAILABLE] ollama down: connection refused"
	if e.Error() != want {
		t.Errorf("expected error %q, got %q", want, e.Error())
	}
}

func TestNewBadQueryError(t *testing.T) {
	e := NewBadQueryError("empty text")
	if e.Type != ErrBadQuery {
		t.Errorf("expected type %q, got %q", ErrBadQuery, e.Type)
	}
	want := "[BAD_QUERY] empty text"
	if e.Error() != want {
		t.Errorf("expected error %q, got %q", want, e.Error())
	}
}

func TestIsBusy(t *testing.T) {
	if !IsBusy(NewBusyError("x")) {
		t.Error("expected IsBusy true for Busy error")
	}
	if IsBusy(NewUnavailableError("x", nil)) {
		t.Error("expected IsBusy false for Unavailable error")
	}
	if IsBusy(errors.New("random")) {
		t.Error("expected IsBusy false for generic error")
	}
	if IsBusy(nil) {
		t.Error("expected IsBusy false for nil")
	}
}

func TestIsUnavailable(t *testing.T) {
	if !IsUnavailable(NewUnavailableError("x", nil)) {
		t.Error("expected IsUnavailable true")
	}
	if IsUnavailable(NewBusyError("x")) {
		t.Error("expected IsUnavailable false for Busy")
	}
	if IsUnavailable(nil) {
		t.Error("expected IsUnavailable false for nil")
	}
}

func TestIsBadQuery(t *testing.T) {
	if !IsBadQuery(NewBadQueryError("x")) {
		t.Error("expected IsBadQuery true")
	}
	if IsBadQuery(NewBusyError("x")) {
		t.Error("expected IsBadQuery false for Busy")
	}
	if IsBadQuery(nil) {
		t.Error("expected IsBadQuery false for nil")
	}
}
