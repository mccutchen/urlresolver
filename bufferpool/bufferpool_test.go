package bufferpool

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Kind of a silly/pointless test, but it should satisfy codecov
func TestBufferPool(t *testing.T) {
	p := New()
	b := p.Get()
	n, err := b.Write([]byte("foo"))
	assert.Equal(t, len("foo"), n)
	assert.NoError(t, err)
	p.Put(b)
}
