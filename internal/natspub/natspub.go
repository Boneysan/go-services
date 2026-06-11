// Package natspub is the thin publishing seam between Go services and NATS.
//
// Services that emit events (sheet-api invalidations, gm-api commands) depend
// on the Publisher interface so handlers can be unit-tested with a fake; the
// process wires in a real connection from Connect at startup.
package natspub

import (
	"time"

	"github.com/nats-io/nats.go"
)

type Publisher interface {
	Publish(subject string, data []byte) error
}

// Connect dials NATS in the background. With RetryOnFailedConnect the
// returned connection is usable immediately and buffers publishes until the
// broker is reachable — services must stay up when NATS is down (sheet
// invalidation is advisory; the EGS full-reloads on reconnect as a failsafe).
func Connect(url, name string) (*nats.Conn, error) {
	return nats.Connect(url,
		nats.Name(name),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
}

// Noop discards publishes; used when NATS is explicitly disabled.
type Noop struct{}

func (Noop) Publish(string, []byte) error { return nil }
