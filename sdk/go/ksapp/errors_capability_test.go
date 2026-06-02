package ksapp

import (
	"errors"
	"strings"
	"testing"
)

func TestKeystoneErrorWraps(t *testing.T) {
	cases := []struct {
		err    error
		isKind error
		name   string
	}{
		{ErrTokenInvalid, ErrAuthError, "TokenInvalid is AuthError"},
		{ErrTokenExpired, ErrAuthError, "TokenExpired is AuthError"},
		{ErrTokenAudienceMismatch, ErrAuthError, "TokenAudienceMismatch is AuthError"},
		{ErrCapabilityForbidden, ErrPermissionError, "CapabilityForbidden is PermissionError"},
		{ErrCapabilityNotFound, ErrNotFoundError, "CapabilityNotFound is NotFoundError"},
		{ErrTaskNotFound, ErrNotFoundError, "TaskNotFound is NotFoundError"},
		{ErrInvalidArgs, ErrValidationError, "InvalidArgs is ValidationError"},
		{ErrManifestMismatch, ErrValidationError, "ManifestMismatch is ValidationError"},
		{ErrCapabilityUnavailable, ErrDependencyError, "CapabilityUnavailable is DependencyError"},
		{ErrLoopDetected, ErrDependencyError, "LoopDetected is DependencyError"},
		{ErrBackendError, ErrExecutionError, "BackendError is ExecutionError"},
		{ErrTimeout, ErrExecutionError, "Timeout is ExecutionError"},
		{ErrCancelled, ErrExecutionError, "Cancelled is ExecutionError"},
		{ErrCapabilityConcurrencyLimit, ErrRateLimitError, "CapabilityConcurrencyLimit is RateLimitError"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !errors.Is(c.err, c.isKind) {
				t.Fatalf("expected %v to wrap %v", c.err, c.isKind)
			}
			if !errors.Is(c.err, ErrKeystoneError) {
				t.Fatalf("expected %v to wrap ErrKeystoneError", c.err)
			}
		})
	}
}

func TestNewCapabilityNotFoundCarriesCanonicalName(t *testing.T) {
	err := NewCapabilityNotFound("ks.x.foo")
	var cnfErr *CapabilityNotFoundErr
	if !errors.As(err, &cnfErr) {
		t.Fatalf("expected *CapabilityNotFoundErr; got %T", err)
	}
	if cnfErr.CanonicalName != "ks.x.foo" {
		t.Fatalf("canonical_name = %q want ks.x.foo", cnfErr.CanonicalName)
	}
	if !errors.Is(err, ErrCapabilityNotFound) {
		t.Fatalf("should wrap ErrCapabilityNotFound sentinel")
	}
}

func TestNewTokenAudienceMismatchMessage(t *testing.T) {
	err := NewTokenAudienceMismatch("expected=foo got=bar")
	if !errors.Is(err, ErrTokenAudienceMismatch) {
		t.Fatalf("should wrap ErrTokenAudienceMismatch")
	}
	if err.Error() == "" {
		t.Fatal("error message should be non-empty")
	}
}

func TestNewManifestMismatch(t *testing.T) {
	err := NewManifestMismatch("ks.x.unregistered", []string{"ks.x.known1", "ks.x.known2"})
	if !errors.Is(err, ErrManifestMismatch) {
		t.Fatalf("should wrap ErrManifestMismatch")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ks.x.unregistered") || !strings.Contains(msg, "ks.x.known1") {
		t.Fatalf("message %q missing fields", msg)
	}
}
