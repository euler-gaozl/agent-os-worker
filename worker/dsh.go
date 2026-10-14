package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/euler-gaozl/agent-os-runtime/execution"
	"os"
	"os/exec"
	"path/filepath"
)

const EngineDSHEvidence = execution.EngineDSHEvidence

type harnessFrame = execution.Frame

var errDSHWaiting = execution.ErrWaitingChildren

// driveDSH supervises the official DSH kernel; it contains no model/tool decision loop.
// Each new process replays committed bridge exchanges. Input drift fails closed.
func (w *Worker) driveDSH(ctx context.Context, r *Run) error {
	r.Step = 0 // replay reconstructs the count; cached responses consume no provider requests.
	node := os.Getenv("AGENTOS_NODE")
	if node == "" {
		node = "node"
	}
	entry := os.Getenv("DSH_RUNNER")
	if isSourceEngine(r.Engine) {
		verified, err := w.sourceForEngine(r.Engine)
		if err != nil {
			return err
		}
		entry = verified.entry
	}
	if entry == "" {
		entry = "harness/runner.mjs"
	}
	entry, e := filepath.Abs(entry)
	if e != nil {
		return e
	}
	if _, e = os.Stat(entry); e != nil {
		return fmt.Errorf("DSH runner unavailable: %w", e)
	}
	defs, e := ToolDefinitions(r.Snapshot)
	if e != nil {
		return e
	}
	cmd := exec.CommandContext(ctx, node, "--max-old-space-size=192", entry)
	if isSourceEngine(r.Engine) {
		cmd = exec.CommandContext(ctx, node, "--max-old-space-size=192", "--no-global-search-paths", entry)
		cmd.Dir = filepath.Dir(entry)
	}
	// The harness gets no service/database/model credentials. Tool and model calls cross the pipe.
	for _, key := range []string{"PATH", "SystemRoot", "WINDIR", "TEMP", "TMP", "LANG"} {
		if value := os.Getenv(key); value != "" {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.Env = append(cmd.Env, "DSH_TELEMETRY_DISABLED=1")
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	stdin, e := cmd.StdinPipe()
	if e != nil {
		return e
	}
	cmd.Stderr = os.Stderr
	if e = cmd.Start(); e != nil {
		return e
	}
	defer func() { stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	original := *r
	original.Constraints = append(append([]string{}, r.Snapshot.Agent.Constraints...), r.State.Pinned...)
	contextTokens := r.Snapshot.Agent.ContextTokens
	if contextTokens == 0 {
		contextTokens = 32000
	}
	init := map[string]any{"type": "init", "run_id": r.ID, "task": r.Task, "system": SystemMessage(original).Content, "model": r.Snapshot.Agent.Model.Model, "context_tokens": contextTokens, "tools": defs}
	if r.Engine == EngineDSHEvidence || isSourceEngine(r.Engine) {
		init["evidence_protocol"] = 1
	}
	if isSourceEngine(r.Engine) {
		init["engine"] = r.Engine
	}
	if r.Engine == EngineDSHBudget {
		output := r.Snapshot.Agent.Model.MaxTokens
		if output == 0 {
			output = 4096
		}
		init["model_max_tokens"] = output
	}
	if _, e = fmt.Fprintln(stdin, string(JSON(init))); e != nil {
		return e
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 65536), 32<<20)
	expected := 1
	for scanner.Scan() {
		var f harnessFrame
		if e = json.Unmarshal(scanner.Bytes(), &f); e != nil {
			return fmt.Errorf("DSH protocol: %w", e)
		}
		if f.Method == "failed" && r.Engine != EngineDSHEvidence && !isSourceEngine(r.Engine) {
			return fmt.Errorf("DSH failed: %s", f.Params)
		}
		if f.ID != expected {
			return errors.New("DSH bridge sequence mismatch")
		}
		expected++
		result, done, e := w.harnessExchange(ctx, r, f)
		if errors.Is(e, errDSHWaiting) {
			return nil
		}
		if e != nil {
			return e
		}
		if _, e = fmt.Fprintln(stdin, string(JSON(map[string]any{"id": f.ID, "result": result}))); e != nil {
			return e
		}
		if done {
			return nil
		}
	}
	if e = scanner.Err(); e != nil {
		return e
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.New("DSH exited before a committed result; inspect worker log")
}

func (w *Worker) harnessExchange(ctx context.Context, r *Run, f harnessFrame) (any, bool, error) {
	return w.Store.Exchange(ctx, r, f, w)
}
