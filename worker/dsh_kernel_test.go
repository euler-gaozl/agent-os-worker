package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/euler-gaozl/agent-os-runtime/execution"
)

// This is the complete, pinned DSH kernel with real model/ToolHub HTTP
// transports, but an in-memory exchange/ledger fixture. SQL recovery and
// old/new executable handoff are separate integration acceptance tests.
func TestPinnedDSHKernelDrivesModelToolAndFinishThroughWorker(t *testing.T) {
	entry, e := filepath.Abs("../harness/runner.mjs")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(filepath.Dir(entry), "node_modules", "@deepseek-ai", "dsh-agent-loop")); e != nil {
		if os.Getenv("AGENTOS_REQUIRE_DSH") == "1" {
			t.Fatal("required pinned DSH dependencies unavailable")
		}
		t.Skip("install pinned harness dependencies to execute kernel test")
	}
	t.Setenv("DSH_RUNNER", entry)
	for _, engine := range []string{"dsh-0.2.0-rc.2", EngineDSHEvidence} {
		t.Run(engine, func(t *testing.T) {
			var models, tools atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Messages []Message         `json:"messages"`
					Tools    []json.RawMessage `json:"tools"`
				}
				if r.URL.Path != "/chat/completions" || json.NewDecoder(r.Body).Decode(&request) != nil {
					w.WriteHeader(400)
					return
				}
				models.Add(1)
				message := Message{Role: "assistant"}
				finish := "tool_calls"
				if len(request.Messages) > 0 && request.Messages[len(request.Messages)-1].Role == "tool" {
					if request.Messages[len(request.Messages)-1].ToolCallID != "save-fixture" {
						t.Error("DSH lost tool-call identity")
					}
					message.Content = "fixture evidence saved"
					finish = "stop"
				} else {
					if len(request.Tools) != 1 {
						t.Error("DSH exposed tools beyond bound snapshot")
					}
					message.ToolCalls = []ToolCall{{ID: "save-fixture", Type: "function", Function: FunctionCall{Name: "note_save", Arguments: `{"kind":"fact","title":"Kernel fixture","content":"fixture only","source":"test://worker-kernel"}`}}}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": message, "finish_reason": finish}}})
			}))
			defer provider.Close()
			hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input Invocation
				if r.URL.Path != "/invoke" || r.Header.Get("Authorization") != "Bearer fixture-worker-token" || json.NewDecoder(r.Body).Decode(&input) != nil || input.Name != "note_save" || input.RunID != "fixture-run" || input.Fence != 7 {
					t.Error("tool envelope/identity changed")
					w.WriteHeader(400)
					return
				}
				tools.Add(1)
				_ = json.NewEncoder(w).Encode(map[string]any{"saved": true, "fixture": true})
			}))
			defer hub.Close()
			ledger := &modelLedger{}
			methods := []string{}
			cursor := 0
			p := hostPersistence{Persistence: ledger, exchange: func(ctx context.Context, r *Run, f execution.Frame, effects execution.Effects) (any, bool, error) {
				methods = append(methods, f.Method)
				if engine == EngineDSHEvidence {
					var events []json.RawMessage
					if json.Unmarshal(f.Events, &events) != nil || f.EventStart != cursor || f.EventEnd < cursor || f.EventEnd-f.EventStart != len(events) {
						return nil, false, errors.New("non-contiguous kernel events")
					}
					cursor = f.EventEnd
				}
				switch f.Method {
				case "model":
					var params struct {
						Messages  []Message `json:"messages"`
						Tools     []Tool    `json:"tools"`
						MaxTokens int       `json:"max_tokens"`
					}
					if e := json.Unmarshal(f.Params, &params); e != nil {
						return nil, false, e
					}
					defs, e := ToolDefinitions(r.Snapshot)
					if e != nil {
						return nil, false, e
					}
					if len(params.Tools) != 1 || len(defs) != 1 || params.Tools[0].Name != defs[0].Name || string(JSON(params.Tools[0].InputSchema)) != string(JSON(defs[0].InputSchema)) {
						return nil, false, errors.New("kernel frame tool definitions differ from fixed snapshot")
					}
					modelRun := *r
					modelRun.Snapshot.Agent.Model.MaxTokens = params.MaxTokens
					if cap := r.Snapshot.Agent.Model.MaxTokens; cap > 0 && (params.MaxTokens == 0 || cap < params.MaxTokens) {
						modelRun.Snapshot.Agent.Model.MaxTokens = cap
					}
					m, e := effects.CompleteModel(ctx, modelRun, fmt.Sprintf("fixture-run/dsh/%d", f.ID), params.Messages, params.Tools)
					return m, false, e
				case "tool":
					var params struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					}
					if e := json.Unmarshal(f.Params, &params); e != nil {
						return nil, false, e
					}
					result, e := effects.InvokeTool(ctx, Invocation{RunID: r.ID, Owner: r.Owner, Fence: r.Fence, RuleRevision: r.RuleRevision, OperationID: fmt.Sprintf("fixture-run/dsh/%d", f.ID), Name: params.Name, Arguments: params.Arguments})
					return string(result), false, e
				case "finish":
					var params struct {
						Result string `json:"result"`
					}
					if e := json.Unmarshal(f.Params, &params); e != nil || params.Result != "fixture evidence saved" {
						return nil, false, errors.New("kernel result mismatch")
					}
					return map[string]bool{"done": true}, true, nil
				default:
					return nil, false, fmt.Errorf("unexpected kernel method %s", f.Method)
				}
			}}
			run := providerRun(provider.URL)
			run.Engine = engine
			run.Task = "Save fixture evidence."
			run.Snapshot.Agent.Tools = []string{"note_save"}
			run.Snapshot.Agent.ContextTokens = 32000
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			worker := Worker{Store: p, Secrets: &fixtureSecrets{}, ToolHubURL: hub.URL, Token: "fixture-worker-token"}
			if e := worker.driveDSH(ctx, &run); e != nil {
				t.Fatal(e)
			}
			if models.Load() != 2 || tools.Load() != 1 || ledger.begins != 2 || ledger.ends != 2 || fmt.Sprint(methods) != "[model tool model finish]" {
				t.Fatalf("kernel dispatch mismatch: %v models=%d tools=%d", methods, models.Load(), tools.Load())
			}
			if engine == EngineDSHEvidence && cursor == 0 {
				t.Fatal("no evidence events emitted")
			}
		})
	}
}
