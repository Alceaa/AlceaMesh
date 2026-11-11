package transfer

import (
	"context"
	"time"
)

type pacer struct {
	bytesPerSecond int64
	total          int64
	started        time.Time
}

func newPacer(bytesPerSecond int64) *pacer {
	if bytesPerSecond <= 0 {
		return nil
	}
	return &pacer{bytesPerSecond: bytesPerSecond, started: time.Now()}
}

func (p *pacer) wait(ctx context.Context, n int) error {
	if p == nil || n <= 0 {
		return nil
	}
	p.total += int64(n)
	target := time.Duration(float64(p.total) / float64(p.bytesPerSecond) * float64(time.Second))
	delay := target - time.Since(p.started)
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
