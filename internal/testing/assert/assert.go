// Package assert implements common assertions used in go-httbin's unit tests.
package assert

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// Equal asserts that two values are equal.
func Equal[T comparable](t testing.TB, got, want T, desc ...string) {
	t.Helper()
	if got != want {
		msg := ""
		if len(desc) > 0 {
			msg = strings.Join(desc, " ")
		}
		t.Fatalf("%s:\nwant: %v\n got: %v", msg, want, got)
	}
}

// DeepEqual asserts that two values are deeply equal.
func DeepEqual[T any](t testing.TB, got, want T, desc ...string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		msg := ""
		if len(desc) > 0 {
			msg = strings.Join(desc, " ")
		}
		t.Fatalf("%s:\nwant: %#v\n got: %#v", msg, want, got)
	}
}

// Error asserts that an error matches an expected error or any one of a list
// of expected errors.
func Error(t testing.TB, got, want error) {
	t.Helper()
	switch {
	case want == nil && got != nil:
		t.Fatalf("want nil error, got %q (%T)", got, got)
	case want != nil && got == nil:
		t.Fatalf("got nil error, want %q (%T)", want, want)
	case want == nil && got == nil:
		return
	case errors.Is(got, want):
		return
	case got.Error() == want.Error():
		return
	default:
		t.Fatalf("want error %q, got %q (%T vs %T)", want, got, want, got)
	}
}

// NilError asserts that an error is nil.
func NilError(t testing.TB, err error) {
	t.Helper()
	Error(t, err, nil)
}
