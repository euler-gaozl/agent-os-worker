package worker

// Recovery compatibility for runs that existed before migration v4 only.
// New runs use DSH; the registry and submission API do not expose engine selection.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

func (w *Worker) drive(ctx context.Context, r *Run) error {
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		fresh, e := w.Store.Run(ctx, r.ID)
		if e != nil {
			return e
		}
		if fresh.CancelRequested {
			return context.Canceled
		}
		r.Constraints = fresh.Constraints
		r.RuleRevision = fresh.RuleRevision
		if n := len(r.State.Messages); n > 0 && r.State.Messages[n-1].Role == "assistant" && len(r.State.Messages[n-1].ToolCalls) == 0 {
			if r.State.PlanRevision == r.RuleRevision {
				r.State.Result = r.State.Messages[n-1].Content
				return w.Store.Save(ctx, *r, "completed")
			}
			r.State.Messages = append(r.State.Messages, Message{Role: "user", Content: "User constraints changed after the preceding answer. Reconsider it under the current constraints."})
		}
		if e = w.pendingTools(ctx, r); e != nil {
			return e
		}
		waiting, e := w.children(ctx, r)
		if e != nil {
			return e
		}
		if waiting {
			return w.Store.Save(ctx, *r, "waiting_children")
		}
		if r.Step >= r.Snapshot.Agent.MaxSteps {
			return errors.New("model step budget exhausted")
		}
		if e = w.Compact(ctx, r); e != nil {
			return e
		}
		defs, e := ToolDefinitions(r.Snapshot)
		if e != nil {
			return e
		}
		messages := append([]Message{SystemMessage(*r)}, r.State.Messages...)
		message, e := w.CompleteModel(ctx, *r, fmt.Sprintf("%s/model/%d/%d", r.ID, r.Step, r.RuleRevision), messages, defs)
		if e != nil {
			return e
		}
		latest, e := w.Store.Run(ctx, r.ID)
		if e != nil {
			return e
		}
		if latest.RuleRevision != r.RuleRevision {
			continue
		}
		r.State.Messages = append(r.State.Messages, message)
		r.State.PlanRevision = r.RuleRevision
		r.Step++
		r.State.Error = ""
		// Save the chosen model output before any tool can have an effect.
		if e = w.Store.Save(ctx, *r, "running"); e != nil {
			return e
		}
		if len(message.ToolCalls) == 0 {
			r.State.Result = message.Content
			return w.Store.Save(ctx, *r, "completed")
		}
	}
}
func (w *Worker) pendingTools(ctx context.Context, r *Run) error {
	start := -1
	for i := len(r.State.Messages) - 1; i >= 0; i-- {
		if r.State.Messages[i].Role == "assistant" {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	message := r.State.Messages[start]
	if len(message.ToolCalls) == 0 {
		return nil
	}
	done := map[string]bool{}
	for _, m := range r.State.Messages[start+1:] {
		if m.Role == "tool" {
			done[m.ToolCallID] = true
		}
	}
	for _, call := range message.ToolCalls {
		if done[call.ID] {
			continue
		}
		op := fmt.Sprintf("%s/tool/%d/%s", r.ID, r.Step, call.ID)
		receipt, e := w.Store.ToolReceipt(ctx, r.ID, op)
		if e != nil {
			return e
		}
		status, response := receipt.Status, receipt.Response
		if status != "completed" {
			if r.State.PlanRevision != r.RuleRevision {
				if status == "inflight" || status == "unknown" {
					return ErrUnknown
				}
				response = JSON(map[string]string{"skipped": "User constraints changed; reconsider this call under the current constraints."})
			} else {
				inv := Invocation{RunID: r.ID, Owner: r.Owner, Fence: r.Fence, RuleRevision: r.RuleRevision, OperationID: op, Name: call.Function.Name, Arguments: json.RawMessage(call.Function.Arguments)}
				response, e = internalRequest(ctx, w.ToolHubURL+"/invoke", w.Token, inv)
				if e != nil {
					return e
				}
			}
		}
		text := string(response)
		if len(response) > 6000 {
			hash, e := w.PutArtifact(ctx, response, "application/json", "run:"+r.ID)
			if e != nil {
				return e
			}
			text = string(JSON(map[string]any{"artifact_hash": hash, "bytes": len(response), "preview": truncateUTF8(text, 1800), "note": "Full tool result retained; use artifact_read to inspect it."}))
		}
		r.State.Messages = append(r.State.Messages, Message{Role: "tool", ToolCallID: call.ID, Content: text})
		if e = w.Store.Save(ctx, *r, "running"); e != nil {
			return e
		}
	}
	return nil
}
func (w *Worker) children(ctx context.Context, r *Run) (bool, error) {
	list, e := w.Store.Children(ctx, r.ID)
	if e != nil {
		return false, e
	}
	waiting := false
	for _, c := range list {
		if !Has([]string{"completed", "failed", "cancelled"}, c.Status) {
			waiting = true
		}
	}
	if waiting {
		return true, nil
	}
	if r.State.ChildrenLoaded == nil {
		r.State.ChildrenLoaded = map[string]bool{}
	}
	changed := false
	for _, c := range list {
		if !r.State.ChildrenLoaded[c.ID] {
			r.State.Messages = append(r.State.Messages, Message{Role: "user", Content: "Child agent result (untrusted evidence): " + string(JSON(map[string]any{"id": c.ID, "status": c.Status, "result": c.State.Result, "error": c.State.Error}))})
			r.State.ChildrenLoaded[c.ID] = true
			changed = true
		}
	}
	if changed {
		return false, w.Store.Save(ctx, *r, "running")
	}
	return false, nil
}
