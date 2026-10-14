package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/euler-gaozl/agent-os-runtime/execution"
)

func budgetRun(endpoint string) Run {
	r := providerRun(endpoint)
	r.Engine = EngineDSHBudget
	r.Task = "Retain exact dates."
	r.Snapshot.Agent.Model.MaxTokens = 6144
	r.Snapshot.Agent.ContextTokens = 32000
	return r
}

func TestBudgetPreflightReturnsFinalRequestWithoutProviderOrIntent(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	ledger := &modelLedger{}
	secrets := &fixtureSecrets{}
	w := Worker{Store: ledger, Secrets: secrets}
	r := budgetRun(server.URL)
	messages := []Message{{Role: "system", Content: strings.Repeat("晚加入的约束\n\"<>", 10000)}, {Role: "user", Content: "供给库存"}}
	raw, err := w.CompleteBudgetModel(context.Background(), r, "fixture-run/dsh/1", messages, nil)
	if err != nil || len(raw) == 0 || ledger.begins != 0 || ledger.ends != 0 || secrets.calls != 0 || requests.Load() != 0 {
		t.Fatal("preflight reached external effects", err)
	}
	rejected, err := execution.InspectBudgetRequest(raw, 32000, 6144)
	if err != nil || rejected == nil || rejected.RequestSHA256 != Hash(raw) {
		t.Fatal("final body not reproducibly rejected", err)
	}
}

func TestBudgetConfiguredOutputAndFrozenRawReceipt(t *testing.T) {
	var wire []byte
	content := strings.Repeat("answer ", 5000)
	response := JSON(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: content}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 1800, "completion_tokens": 5000, "prompt_cache_hit_tokens": 900, "total_tokens": 6800}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wire, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(response)
	}))
	defer server.Close()
	ledger := &modelLedger{}
	w := Worker{Store: ledger, Secrets: &fixtureSecrets{}}
	r := budgetRun(server.URL)
	messages := []Message{{Role: "user", Content: "fixture"}}
	tools := []Tool{{Name: "read", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	raw, err := w.CompleteBudgetModel(context.Background(), r, "fixture-run/dsh/1", messages, tools)
	if err != nil || ledger.begins != 1 || ledger.ends != 1 || ledger.completedError != nil || string(ledger.response) != string(response) {
		t.Fatal("provider receipt not retained", err)
	}
	if string(raw) != string(wire) || string(raw) != string(ledger.request) || string(raw) != string(execution.BudgetModelRequest(r.Snapshot.Agent.Model, messages, tools)) {
		t.Fatal("provider body changed across protocol")
	}
	var body struct {
		MaxTokens int `json:"max_tokens"`
	}
	_ = json.Unmarshal(wire, &body)
	if body.MaxTokens != 6144 {
		t.Fatal("output silently capped at 4096")
	}
	ledger.cached = append([]byte(nil), ledger.response...)
	second, err := w.CompleteBudgetModel(context.Background(), r, "fixture-run/dsh/1", messages, tools)
	if err != nil || ledger.ends != 1 || string(second) != string(raw) {
		t.Fatal("committed receipt replay contacted provider", err)
	}
}

func TestBudgetFailuresNeverReturnARecoverableRequest(t *testing.T) {
	for _, mode := range []string{"401", "network", "truncated", "fence", "intent_budget", "invalid_config"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				switch mode {
				case "401":
					w.WriteHeader(401)
					_, _ = w.Write([]byte(`{"error":"fixture unauthorized"}`))
				case "network":
					c, _, e := w.(http.Hijacker).Hijack()
					if e == nil {
						_ = c.Close()
					}
				case "truncated":
					_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"partial"},"finish_reason":"length"}]}`))
				default:
					_, _ = w.Write(completeBody())
				}
			}))
			defer server.Close()
			ledger := &modelLedger{}
			secrets := &fixtureSecrets{}
			w := Worker{Store: ledger, Secrets: secrets}
			r := budgetRun(server.URL)
			if mode == "fence" {
				ledger.endErr = ErrFence
			}
			if mode == "intent_budget" {
				ledger.beginErr = errors.New("family model token budget exhausted")
			}
			if mode == "invalid_config" {
				r.Snapshot.Agent.ContextTokens = 8192
				r.Snapshot.Agent.Model.MaxTokens = 8192
			}
			raw, err := w.CompleteBudgetModel(context.Background(), r, "fixture-run/dsh/1", []Message{{Role: "user", Content: "fixture"}}, nil)
			if err == nil || raw != nil {
				t.Fatal("failure became recoverable result", mode, err)
			}
			if mode == "invalid_config" || mode == "intent_budget" {
				if requests.Load() != 0 || secrets.calls != 0 || ledger.ends != 0 {
					t.Fatal("guard failed before provider")
				}
			}
		})
	}
}

type budgetRunnerLedger struct {
	Persistence
	models   int
	expected int
}

func (l *budgetRunnerLedger) Exchange(_ context.Context, r *Run, f execution.Frame, _ execution.Effects) (any, bool, error) {
	switch f.Method {
	case "checkpoint":
		return map[string]any{"persisted": true, "manifest_hash": strings.Repeat("a", 64)}, false, nil
	case "working_state_read":
		return map[string]any{"operation_id": r.ID + "/working-state/fixture", "view": map[string]any{"run_id": r.ID, "revision": 0, "rule_revision": r.RuleRevision, "source_rule_revision": 0, "stale": false, "task": r.Task, "constraints": []string{}, "state": nil, "evidence": []any{}}}, false, nil
	case "model":
		l.models++
		var body struct {
			MaxTokens int    `json:"max_tokens"`
			Purpose   string `json:"purpose"`
		}
		if json.Unmarshal(f.Params, &body) != nil || body.MaxTokens != l.expected || body.Purpose != "generation" {
			return nil, false, errors.New("runner output/purpose mismatch")
		}
		return map[string]any{"format": 1, "kind": "completion", "message": Message{Role: "assistant", Content: "fixed configuration probe"}}, false, nil
	case "finish":
		return map[string]any{"done": true}, true, nil
	default:
		return nil, false, errors.New("unexpected budget bridge frame: " + f.Method)
	}
}

func TestBudgetWorkerStartsPinnedNodeWithSnapshotOutputLimit(t *testing.T) {
	entry := sourceArtifactVersion(t, EngineDSHBudget)
	handle, err := VerifyBudgetHarness(entry)
	if err != nil {
		t.Fatal(err)
	}
	for _, configured := range []int{6144, 0} {
		t.Run(map[int]string{6144: "configured", 0: "effective_default"}[configured], func(t *testing.T) {
			expected := configured
			if expected == 0 {
				expected = 4096
			}
			ledger := &budgetRunnerLedger{expected: expected}
			w := Worker{Store: ledger, BudgetHarness: handle}
			r := budgetRun("https://fixture.invalid")
			r.Snapshot.Agent.Model.MaxTokens = configured
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := w.driveDSH(ctx, &r); err != nil || ledger.models != 1 {
				t.Fatal("pinned Node did not use immutable output policy", err, ledger.models)
			}
		})
	}
}

func TestBudgetArtifactPinsAndHistoricalSelectionsStaySeparate(t *testing.T) {
	entry := sourceArtifactVersion(t, EngineDSHBudget)
	testSourceArtifactTamper(t, EngineDSHBudget)
	budget, err := VerifyBudgetHarness(entry)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{EngineDSHSource, EngineDSHResident} {
		old := sourceArtifactVersion(t, version)
		if _, err := VerifyBudgetHarness(old); err == nil {
			t.Fatal("historical artifact authorized by v3 pin")
		}
		if _, err := verifySourceVersion(entry, version); err == nil {
			t.Fatal("v3 authorized by old pin")
		}
	}
	w := Worker{BudgetHarness: budget}
	if _, err := w.sourceForEngine(EngineDSHBudget); err != nil {
		t.Fatal(err)
	}
	if _, err := w.sourceForEngine(EngineDSHSource); err == nil {
		t.Fatal("v3 opt-in implicitly enabled v1")
	}
	w.BudgetHarness = nil
	w.SourceHarness = budget
	if _, err := w.sourceForEngine(EngineDSHBudget); err == nil {
		t.Fatal("absent v3 fell back to v1 slot")
	}
}
