package model

import (
	"bytes"
	"io"
	"sync"
)

// Media owns an immutable buffer. Its reservation lives until all readers close.
type Media struct {
	File    File
	mu      sync.Mutex
	data    []byte
	refs    int
	release func()
	closed  bool
}

func NewMedia(file File, data []byte, release func()) *Media {
	return &Media{File: file, data: data, refs: 1, release: release}
}
func (m *Media) Size() int64 { m.mu.Lock(); defer m.mu.Unlock(); return int64(len(m.data)) }
func (m *Media) Open() (io.ReadSeekCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, io.ErrClosedPipe
	}
	m.refs++
	return &mediaReader{Reader: bytes.NewReader(m.data), media: m}, nil
}
func (m *Media) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		m.drop()
	}
}
func (m *Media) drop() {
	m.refs--
	if m.refs == 0 {
		m.data = nil
		if m.release != nil {
			m.release()
		}
	}
}

type mediaReader struct {
	*bytes.Reader
	media *Media
	once  sync.Once
}

func (r *mediaReader) Close() error {
	r.once.Do(func() { r.Reader.Reset(nil); r.media.mu.Lock(); defer r.media.mu.Unlock(); r.media.drop() })
	return nil
}
