package bufferpool

import (
	"testing"

	"github.com/mccutchen/urlresolver/internal/testing/assert"
)

// Kind of a silly/pointless test, but it should satisfy codecov
func TestBufferPool(t *testing.T) {
	p := New()
	b := p.Get()
	n, err := b.Write([]byte("foo"))
	assert.Equal(t, n, len("foo"))
	assert.NilError(t, err)
	p.Put(b)
}
