// Package clock holds the one timing helper the pollers share.
package clock

import (
	"context"
	"time"
)

// Sleep pauses for d or until ctx is done, whichever comes first, and
// returns ctx.Err() in the latter case. Use it in polling loops in place of
// time.Sleep so cancellation and deadlines are honoured immediately rather
// than at the end of the current pause.
func Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
