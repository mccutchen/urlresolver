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

// NilError asserts that an error is nil.
func NilError(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("expected nil error, got %q (%T)", err, err)
	}
}

// Error asserts that an error matches an expected error or any one of a list
// of expected errors.
func Error(t testing.TB, got, expected error, alternates ...error) {
	t.Helper()
	matched := false
	wantAny := append([]error{expected}, alternates...)
	for _, want := range wantAny {
		if errorsMatch(t, got, want) {
			matched = true
			break
		}
	}
	if !matched {
		if len(wantAny) == 1 {
			t.Fatalf("expected error %q, got %q (%T vs %T)", expected, got, expected, got)
		} else {
			t.Fatalf("expected one of %v, got %q (%T)", wantAny, got, got)
		}
	}
}

func errorsMatch(t testing.TB, got, expected error) bool {
	t.Helper()
	if got == nil {
		t.Fatalf("got nil error")
	}
	if expected == nil {
		t.Fatalf("expected error cannot be nil")
	}
	if errors.Is(got, expected) {
		return true
	}
	return got.Error() == expected.Error()
}
