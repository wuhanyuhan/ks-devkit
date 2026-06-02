package exitcode

import (
	"errors"
	"testing"
)

func TestExitErrorCodeAndUnwrap(t *testing.T) {
	inner := errors.New("boom")
	e := Wrap(inner, AuthOrPermission)

	if e.Code() != AuthOrPermission {
		t.Fatalf("Code() = %d, want %d", e.Code(), AuthOrPermission)
	}
	if !errors.Is(e, inner) {
		t.Fatalf("errors.Is(e, inner) want true")
	}
	want := "boom"
	if e.Error() != want {
		t.Fatalf("Error() = %q, want %q", e.Error(), want)
	}
}

func TestExtractReturnsZeroForNil(t *testing.T) {
	if got := Extract(nil); got != Success {
		t.Fatalf("Extract(nil) = %d, want %d", got, Success)
	}
}

func TestExtractReturnsGenericForPlainError(t *testing.T) {
	if got := Extract(errors.New("x")); got != Generic {
		t.Fatalf("Extract(plain) = %d, want %d", got, Generic)
	}
}

func TestExtractReturnsCodeForExitError(t *testing.T) {
	e := Wrap(errors.New("net"), Network)
	if got := Extract(e); got != Network {
		t.Fatalf("Extract(ExitError) = %d, want %d", got, Network)
	}
}

func TestConstantValues(t *testing.T) {
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"Success", Success, 0},
		{"Generic", Generic, 1},
		{"AuthOrPermission", AuthOrPermission, 2},
		{"ClientConfig", ClientConfig, 3},
		{"Network", Network, 4},
		{"DuplicateVersion", DuplicateVersion, 5},
		{"ReviewRejected", ReviewRejected, 6},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}
