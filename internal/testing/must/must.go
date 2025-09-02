// Package must implements helper functions for testing to eliminate some error
// checking boilerplate.
package must

import (
	"io"
	"testing"
)

// ReadAll reads all bytes from an io.Reader and fails the test if there is an
// error.
func ReadAll(t testing.TB, r io.Reader) string {
	t.Helper()
	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("error reading: %s", err)
	}
	if rc, ok := r.(io.ReadCloser); ok {
		if err := rc.Close(); err != nil {
			t.Fatalf("error closing after reading: %s", err)
		}
	}
	return string(body)
}

