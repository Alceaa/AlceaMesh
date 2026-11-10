package transfer

import "sync"

const copyBufferSize = 512 << 10

var copyBuffers = sync.Pool{
	New: func() any {
		return make([]byte, copyBufferSize)
	},
}

func takeBuffer() []byte {
	return copyBuffers.Get().([]byte)
}

func putBuffer(buffer []byte) {
	if cap(buffer) < copyBufferSize {
		return
	}
	copyBuffers.Put(buffer[:copyBufferSize])
}
