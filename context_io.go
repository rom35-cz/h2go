package h2go

import (
	"context"
	"time"
)

// deadlineSetter is the minimal transport capability needed to apply context
// deadlines to the H2 connection.
type deadlineSetter interface {
	SetDeadline(time.Time) error
}

// beginOperationContext applies an overall deadline to tr when ctx has one and
// arranges for any later ctx cancellation to wake the transport by tightening
// the deadline to time.Now(). When cancel is provided it is launched in a
// separate goroutine so it can best-effort cancel the running statement on a
// side channel without blocking the caller.
func beginOperationContext(ctx context.Context, tr deadlineSetter, cancel func() error) func() {
	if ctx == nil {
		ctx = context.Background()
	}
	if tr == nil {
		return func() {}
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = tr.SetDeadline(deadline)
	}

	stop := make(chan struct{})
	if done := ctx.Done(); done != nil {
		go func() {
			select {
			case <-done:
				if cancel != nil {
					go func() { _ = cancel() }()
				}
				_ = tr.SetDeadline(time.Now())
			case <-stop:
			}
		}()
	}

	return func() {
		close(stop)
		_ = tr.SetDeadline(time.Time{})
	}
}
