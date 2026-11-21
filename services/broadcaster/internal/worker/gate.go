package worker

import (
	"context"
	"errors"

	"github.com/Maxencejules/shortvideo-network/services/observe/client"
)

// ObserveGate admits retries through svn-observe. An empty address refuses.
func ObserveGate(base, token string) RetryGate {
	return observeGate{c: client.New(base, token)}
}

type observeGate struct {
	c *client.Client
}

func (g observeGate) Admit(ctx context.Context, intentID string) error {
	if g.c == nil {
		return errors.New("admission unreachable")
	}
	decision, err := g.c.Admit(ctx, client.KindRetries, 1, intentID)
	if err != nil {
		return errors.New("admission unreachable")
	}
	if !decision.Granted {
		return errors.New("admission refused")
	}
	return nil
}
