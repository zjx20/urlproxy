package hlsboost

import (
	"context"
	"fmt"
	"io"
	"sync"
)

var _ io.ReadSeeker = (*content)(nil)

type content struct {
	mu   sync.Mutex
	seg  *segment
	off  int64
	size int64
	ctx  context.Context
}

func toContent(ctx context.Context, seg *segment, size int64) *content {
	return &content{
		seg:  seg,
		ctx:  ctx,
		size: size,
	}
}

func (c *content) Read(p []byte) (n int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, err = c.seg.ReadAt(c.ctx, p, c.off)
	c.off += int64(n)
	return
}

func (c *content) Seek(offset int64, whence int) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch whence {
	case io.SeekStart:
		if offset < 0 {
			return 0, fmt.Errorf("negative offset")
		}
		c.off = offset
		return c.off, nil
	case io.SeekCurrent:
		v := c.off + offset
		if v < 0 {
			return 0, fmt.Errorf("negative offset")
		}
		c.off = v
		return c.off, nil
	case io.SeekEnd:
		v := c.size + offset
		if v < 0 {
			return 0, fmt.Errorf("negative offset")
		}
		c.off = v
		return c.off, nil
	default:
		return 0, fmt.Errorf("unknown whence %d", whence)
	}
}
