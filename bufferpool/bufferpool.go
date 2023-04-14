package bufferpool

import (
	"bytes"
	"sync"
)

// BufferPool uses a sync.Pool to manage a pool of bytes.Buffer.
type BufferPool struct {
	pool *sync.Pool
}

// New creates a new BufferPool.
func New() *BufferPool {
	return &BufferPool{
		pool: &sync.Pool{
			New: func() interface{} {
				return new(bytes.Buffer)
			},
		},
	}
}

// Get gets a bytes.Buffer from the pool.
func (bp *BufferPool) Get() *bytes.Buffer {
	buf := bp.pool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// Put puts a bytes.Buffer back into the pool.
func (bp *BufferPool) Put(buf *bytes.Buffer) {
	bp.pool.Put(buf)
}
