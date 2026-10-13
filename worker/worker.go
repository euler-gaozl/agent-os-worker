package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/euler-gaozl/agent-os-runtime/execution"
)

type Worker struct {
	Store         Persistence
	Archives      execution.ArtifactWriter
	Secrets       SecretReader
	ID            string
	Lease         time.Duration
	ToolHubURL    string
	RuntimeURL    string
	SourceHarness *SourceHarness
	Token         string
	Poll          time.Duration
}

func (w *Worker) Run(ctx context.Context) error {
	if w.Lease == 0 {
		w.Lease = 20 * time.Second
	}
	if w.Poll == 0 {
		w.Poll = 300 * time.Millisecond
	}
	timer := time.NewTicker(w.Poll)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			r, e := w.Store.Claim(ctx, w.ID, w.Lease)
			if errors.Is(e, execution.ErrNoWork) {
				continue
			}
			if e != nil {
				slog.Error("execution claim unavailable")
				continue
			}
			w.execute(ctx, r)
		}
	}
}
func (w *Worker) execute(parent context.Context, r Run) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	done := make(chan struct{})
	leaseIdentity := Run{ID: r.ID, Owner: r.Owner, Fence: r.Fence}
	defer func() { cancel(); <-done }()
	go func() {
		defer close(done)
		t := time.NewTicker(w.Lease / 3)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if e := w.Store.Heartbeat(ctx, leaseIdentity, w.Lease); e != nil {
					cancel()
					return
				}
			}
		}
	}()
	var err error
	if r.Engine == "legacy-go" {
		err = w.drive(ctx, &r)
	} else if r.Engine == "dsh-0.2.0-rc.2" || r.Engine == EngineDSHEvidence || r.Engine == EngineDSHSource {
		err = w.driveDSH(ctx, &r)
	} else {
		err = errors.New("unsupported execution engine version")
	}
	if err == nil {
		return
	}
	if parent.Err() != nil {
		return
	}
	if e := w.Store.CheckpointFailure(parent, r, err); e != nil {
		slog.Warn("execution checkpoint refused", "run", r.ID)
	}
}

func truncateUTF8(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
