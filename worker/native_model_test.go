package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/euler-gaozl/agent-os-runtime/execution"
	"github.com/euler-gaozl/agent-os-sdk/contracts"
)

func nativeRun(endpoint string) Run {
	r := providerRun(endpoint)
	r.Engine = EngineDSHNative
	r.Task = "供给和库存"
	r.Snapshot.Agent.Model.MaxTokens = 393216
	r.Snapshot.Agent.ContextTokens = 1048576
	r.Snapshot.Agent.MaxModelAttempts = 12
	r.Snapshot.Agent.Model.Limits = &contracts.ModelLimits{Model: r.Snapshot.Agent.Model.Model, BaseURL: endpoint, Protocol: "openai-chat-completions", ContextWindow: 1048576, MaxOutputTokens: 393216, Source: endpoint + "/models", ObservedAt: "2026-10-14T00:00:00Z", MetadataSHA256: strings.Repeat("a", 64)}
	return r
}

func TestNativeTransportLongInputOutputAndCachedProviderReceipt(t *testing.T) {
	var calls atomic.Int32
	var wire []byte
	output := strings.Repeat("事实。", 583334)
	body := JSON(map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: output}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 1000, "completion_tokens": 500}})
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		wire, _ = io.ReadAll(r.Body)
		_, _ = w.Write(body)
	}))
	defer s.Close()
	l := &modelLedger{}
	w := Worker{Store: l, Secrets: &fixtureSecrets{}, nativeClient: s.Client()}
	r := nativeRun(s.URL)
	m := []Message{{Role: "user", Content: strings.Repeat("资料。", 500000)}}
	request, e := w.CompleteNativeModel(context.Background(), r, r.ID+"/dsh/1", m, nil)
	if e != nil || l.ends != 1 || l.completedError != nil || string(request) != string(wire) {
		t.Fatal("native transport failed", e)
	}
	var sent struct {
		Max      int       `json:"max_tokens"`
		Messages []Message `json:"messages"`
	}
	if json.Unmarshal(wire, &sent) != nil || sent.Max != 393216 || sent.Messages[0].Content != m[0].Content {
		t.Fatal("wire cap/input changed")
	}
	receipt, e := execution.ReadNativeReceipt(l.response)
	if e != nil || receipt.HTTPStatus != 200 || string(receipt.Body) != string(body) {
		t.Fatal("complete response not retained", e)
	}
	l.cached = l.response
	if _, e = w.CompleteNativeModel(context.Background(), r, r.ID+"/dsh/1", m, nil); e != nil || calls.Load() != 1 || l.ends != 1 {
		t.Fatal("cached replay contacted provider", e, calls.Load())
	}
}

func TestNativeTransportClassifiesOnlyProvenContextRejection(t *testing.T) {
	for _, mode := range []string{"context_code", "context_message", "generic_400", "401", "429", "network", "partial", "truncated", "invalid_tool", "end_fence", "begin_fence", "invalid_snapshot"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch mode {
				case "context_code":
					w.WriteHeader(400)
					_, _ = w.Write([]byte(`{"error":{"code":"context_length_exceeded"}}`))
				case "context_message":
					w.WriteHeader(400)
					_, _ = w.Write([]byte(`{"error":{"message":"This model's maximum context length is 1048576 tokens. Too much input."}}`))
				case "generic_400":
					w.WriteHeader(400)
					_, _ = w.Write([]byte(`{"error":{"message":"request too large"}}`))
				case "401":
					w.WriteHeader(401)
					_, _ = w.Write([]byte(`{"error":{"code":"context_length_exceeded"}}`))
				case "429":
					w.WriteHeader(429)
					_, _ = w.Write([]byte(`{"error":{"code":"context_length_exceeded"}}`))
				case "network":
					c, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						_ = c.Close()
					}
				case "partial":
					_, _ = w.Write([]byte(`{"error":{"code":"context_length_exceeded"`))
				case "truncated":
					_, _ = w.Write([]byte(strings.ReplaceAll(string(completeBody()), `"stop"`, `"length"`)))
				case "invalid_tool":
					_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"x","type":"function","function":{"name":"read","arguments":"not json"}}]},"finish_reason":"tool_calls"}]}`))
				default:
					_, _ = w.Write(completeBody())
				}
			}))
			defer s.Close()
			l := &modelLedger{}
			w := Worker{Store: l, Secrets: &fixtureSecrets{}, nativeClient: s.Client()}
			r := nativeRun(s.URL)
			if mode == "end_fence" {
				l.endErr = ErrFence
			}
			if mode == "begin_fence" {
				l.beginErr = ErrFence
			}
			if mode == "invalid_snapshot" {
				r.Snapshot.Agent.ContextTokens = 64000
			}
			request, e := w.CompleteNativeModel(context.Background(), r, r.ID+"/dsh/1", []Message{{Role: "user", Content: "fixture"}}, nil)
			allowed := mode == "context_code" || mode == "context_message"
			if allowed {
				if e != nil || len(request) == 0 || l.completedError != nil || l.ends != 1 {
					t.Fatal("context outcome was not durably transportable", e)
				}
				receipt, e := execution.ReadNativeReceipt(l.response)
				if e != nil || !execution.NativeContextRejected(receipt.HTTPStatus, receipt.Body) {
					t.Fatal("missing provider refusal", e)
				}
				l.cached = l.response
				if _, e = w.CompleteNativeModel(context.Background(), r, r.ID+"/dsh/1", []Message{{Role: "user", Content: "fixture"}}, nil); e != nil || calls.Load() != 1 {
					t.Fatal("refusal replay repeated HTTP", e)
				}
			} else if e == nil || request != nil {
				t.Fatal("failure became recoverable rejection", mode, e)
			}
			if (mode == "begin_fence" || mode == "invalid_snapshot") && (calls.Load() != 0 || l.ends != 0) {
				t.Fatal("guard contacted supplier")
			}
		})
	}
}

func TestNativeResponseIdleTimeoutAllowsProgressAndCancellation(t *testing.T) {
	for _, mode := range []string{"progress", "stall", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				if mode != "progress" {
					<-r.Context().Done()
					return
				}
				for i := 0; i < 8; i++ {
					select {
					case <-r.Context().Done():
						return
					case <-time.After(50 * time.Millisecond):
					}
					_, _ = w.Write([]byte("\n"))
					w.(http.Flusher).Flush()
				}
				_, _ = w.Write(completeBody())
			}))
			defer s.Close()
			l := &modelLedger{}
			w := Worker{Store: l, Secrets: &fixtureSecrets{}, nativeClient: s.Client(), nativeIdle: 250 * time.Millisecond}
			r := nativeRun(s.URL)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if mode == "cancel" {
				timer := time.AfterFunc(50*time.Millisecond, cancel)
				defer timer.Stop()
			}
			started := time.Now()
			_, e := w.CompleteNativeModel(ctx, r, r.ID+"/dsh/1", []Message{{Role: "user", Content: "fixture"}}, nil)
			if mode == "progress" {
				if e != nil || time.Since(started) < w.nativeIdle {
					t.Fatal("progress cut by total deadline", e)
				}
			} else if e == nil || l.completedError == nil || l.ends != 1 {
				t.Fatal("stall/cancel not recorded as interrupted", e)
			}
		})
	}
	if nativeHTTP.Timeout != 0 || nativeHTTP.CheckRedirect == nil {
		t.Fatal("native global deadline/redirect regression")
	}
}

type nativeRunnerLedger struct {
	Persistence
	models    int
	task      string
	output    string
	httpState bool
}

func (l *nativeRunnerLedger) Exchange(ctx context.Context, r *Run, f execution.Frame, effects execution.Effects) (any, bool, error) {
	switch f.Method {
	case "checkpoint":
		return map[string]any{"persisted": true, "manifest_hash": strings.Repeat("a", 64)}, false, nil
	case "working_state_read":
		if l.httpState {
			raw, err := effects.(execution.SourceEffects).SourceState(ctx, Invocation{RunID: r.ID, Owner: r.Owner, Fence: r.Fence, RuleRevision: r.RuleRevision, OperationID: r.ID + "/working-state/fixture", Name: f.Method, Arguments: f.Params})
			var value any
			if err != nil {
				return nil, false, err
			}
			if err = json.Unmarshal(raw, &value); err != nil {
				return nil, false, err
			}
			return value, false, nil
		}
		return map[string]any{"operation_id": r.ID + "/working-state/fixture", "view": map[string]any{"run_id": r.ID, "revision": 0, "rule_revision": r.RuleRevision, "source_rule_revision": 0, "stale": false, "task": r.Task, "constraints": []string{}, "state": nil, "evidence": []any{}}}, false, nil
	case "model":
		l.models++
		var body struct {
			Max      int       `json:"max_tokens"`
			Purpose  string    `json:"purpose"`
			Messages []Message `json:"messages"`
		}
		if json.Unmarshal(f.Params, &body) != nil || body.Max != 393216 || body.Purpose != "generation" {
			return nil, false, errors.New("native runner policy mismatch")
		}
		found := false
		for _, m := range body.Messages {
			if strings.Contains(m.Content, l.task) {
				found = true
			}
		}
		if !found {
			return nil, false, errors.New("native initial task truncated")
		}
		return map[string]any{"format": 2, "kind": "completion", "message": Message{Role: "assistant", Content: l.output}}, false, nil
	case "finish":
		var result struct {
			Result string `json:"result"`
		}
		if json.Unmarshal(f.Params, &result) != nil || result.Result != l.output {
			return nil, false, errors.New("native finish truncated")
		}
		return map[string]any{"done": true}, true, nil
	default:
		return nil, false, errors.New("unexpected native runner frame: " + f.Method)
	}
}
func TestNativePinnedNodeHandlesLargeTaskAndResult(t *testing.T) {
	handle, e := VerifyNativeHarness(sourceArtifactVersion(t, EngineDSHNative))
	if e != nil {
		t.Fatal(e)
	}
	l := &nativeRunnerLedger{task: strings.Repeat("资料。", 500000), output: strings.Repeat("事实。", 583334), httpState: true}
	var stateRequests atomic.Int32
	stateServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var inv Invocation
		if req.URL.Path != "/v1/source-state" || json.NewDecoder(req.Body).Decode(&inv) != nil || inv.Name != "working_state_read" {
			w.WriteHeader(400)
			return
		}
		stateRequests.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"operation_id": inv.OperationID, "view": map[string]any{"run_id": inv.RunID, "revision": 0, "rule_revision": inv.RuleRevision, "source_rule_revision": 0, "stale": false, "task": l.task, "constraints": []string{}, "state": nil, "evidence": []any{}}})
	}))
	defer stateServer.Close()
	w := Worker{Store: l, NativeHarness: handle, RuntimeURL: stateServer.URL, Token: "fixture-token"}
	r := nativeRun("https://fixture.invalid")
	r.Task = l.task
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if e = w.driveDSH(ctx, &r); e != nil || l.models != 1 || stateRequests.Load() == 0 {
		t.Fatal("native Node and state HTTP execution failed", e, l.models, stateRequests.Load())
	}
	// Ordinary tool/legacy transports keep their smaller wire capacity.
	if _, e = internalRequest(ctx, stateServer.URL+"/v1/source-state", "fixture-token", Invocation{Name: "working_state_read"}); e == nil {
		t.Fatal("generic internal response cap changed")
	}
}

func TestNativeSendStallCannotKeepLeaseAliveForever(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-release }))
	defer s.Close()
	defer close(release)
	var sent atomic.Bool
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{WroteRequest: func(info httptrace.WroteRequestInfo) {
		if info.Err == nil {
			sent.Store(true)
		}
	}})
	l := &modelLedger{}
	w := Worker{Store: l, Secrets: &fixtureSecrets{}, nativeClient: s.Client(), nativeFirstResponse: 200 * time.Millisecond}
	r := nativeRun(s.URL)
	started := time.Now()
	_, e := w.CompleteNativeModel(ctx, r, r.ID+"/dsh/1", []Message{{Role: "user", Content: strings.Repeat("x", 24<<20)}}, nil)
	if e == nil || l.ends != 1 || l.completedError == nil || time.Since(started) > 3*time.Second {
		t.Fatal("stalled upload not bounded/accounted", e)
	}
	select {
	case <-entered:
	default:
		t.Fatal("TLS request did not reach fixture")
	}
	if sent.Load() {
		t.Fatal("fixture did not exercise a blocked upload")
	}
}
func TestNativePinDoesNotAuthorizeHistoricalArtifacts(t *testing.T) {
	entry := sourceArtifactVersion(t, EngineDSHNative)
	testSourceArtifactTamper(t, EngineDSHNative)
	for _, version := range []string{EngineDSHSource, EngineDSHResident, EngineDSHBudget} {
		if _, e := VerifyNativeHarness(sourceArtifactVersion(t, version)); e == nil {
			t.Fatal("native accepted old runner")
		}
		if _, e := verifySourceVersion(entry, version); e == nil {
			t.Fatal("old pin accepted native runner")
		}
	}
}
