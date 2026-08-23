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

// cancelGracePeriod bounds how long an operation waits, after a side-channel
// cancel has been fired, for the server's own "statement was canceled" report
// to arrive on the main connection. Receiving that aligned H2 error lets the
// session survive the cancellation; if it does not arrive in time (e.g. the
// server is unreachable), the operation times out and the session aborts as
// before.
const cancelGracePeriod = 5 * time.Second

// beginOperationContext applies an overall deadline to tr when ctx has one and
// arranges for any later ctx cancellation to wake the transport by tightening
// the deadline. When cancel is provided it is launched in a separate goroutine
// so it can best-effort cancel the running statement on a side channel without
// blocking the caller; the transport deadline then moves to now+
// cancelGracePeriod so the main connection can still receive (and fully
// parse) the server's aligned cancellation error instead of being cut off
// mid-frame.
func beginOperationContext(ctx context.Context, tr deadlineSetter, cancel func() error) func() {
	if ctx == nil {
		ctx = context.Background()
	}
	if tr == nil {
		return func() {}
	}

	if deadline, ok := ctx.Deadline(); ok {
		effective := deadline
		if cancel != nil {
			// With deep cancellation the server needs time beyond the
			// caller's deadline to stop the statement and report it on the
			// main connection. Pre-extend the transport deadline so the
			// pending response read is not cut off at the very moment the
			// side-channel cancel fires.
			effective = deadline.Add(cancelGracePeriod)
		}
		_ = tr.SetDeadline(effective)
	}

	stop := make(chan struct{})
	if done := ctx.Done(); done != nil {
		go func() {
			select {
			case <-done:
				if cancel != nil {
					go func() { _ = cancel() }()
					_ = tr.SetDeadline(time.Now().Add(cancelGracePeriod))
				} else {
					_ = tr.SetDeadline(time.Now())
				}
			case <-stop:
			}
		}()
	}

	return func() {
		close(stop)
		_ = tr.SetDeadline(time.Time{})
	}
}
