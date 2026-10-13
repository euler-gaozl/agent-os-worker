package worker

import (
	"context"
	"github.com/euler-gaozl/agent-os-runtime/execution"
	"time"
)

// Persistence is consumed by the host; its only production implementation is
// the fixed Runtime execution facade. It never grants raw transaction access.
type Persistence interface {
	Claim(context.Context, string, time.Duration) (Run, error)
	Run(context.Context, string) (Run, error)
	Heartbeat(context.Context, Run, time.Duration) error
	Save(context.Context, Run, string) error
	CheckpointFailure(context.Context, Run, error) error
	BeginModel(context.Context, Run, string, []byte) (string, []byte, error)
	EndModel(context.Context, Run, string, string, []byte, error) error
	ToolReceipt(context.Context, string, string) (execution.Receipt, error)
	Children(context.Context, string) ([]execution.Child, error)
	Exchange(context.Context, *Run, execution.Frame, execution.Effects) (any, bool, error)
}

var _ Persistence = (*execution.Engine)(nil)
var _ execution.SourceEffects = (*Worker)(nil)
