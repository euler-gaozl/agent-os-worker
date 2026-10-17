package worker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Only transport tests use this in-memory ledger. Durable SQL/fencing tests
// belong to Runtime and cross-version executable tests to integration.
type modelLedger struct {
	Persistence
	beginErr, endErr          error
	cached, request, response []byte
	begins, ends              int
	completedError            error
	run                       Run
	operation, attempt        string
}

func (l *modelLedger) BeginModel(_ context.Context, r Run, id string, request []byte) (string, []byte, error) {
	l.begins++
	l.run = r
	l.operation = id
	l.request = append([]byte(nil), request...)
	return "fixture-attempt", l.cached, l.beginErr
}
func (l *modelLedger) EndModel(_ context.Context, r Run, id, attempt string, response []byte, cause error) error {
	l.ends++
	l.run = r
	l.operation = id
	l.attempt = attempt
	l.response = append([]byte(nil), response...)
	l.completedError = cause
	return l.endErr
}

type fixtureSecrets struct {
	calls int
	err   error
}

func (s *fixtureSecrets) Secret(context.Context, string) (string, error) {
	s.calls++
	return "fixture-provider-token-never-real", s.err
}
func providerRun(endpoint string) Run {
	return Run{ID: "fixture-run", Owner: "worker", Fence: 7, RuleRevision: 3, Snapshot: Snapshot{Agent: Agent{Model: ModelConfig{BaseURL: endpoint, Model: "synthetic-provider", MaxTokens: 64, KeyEnv: "FIXTURE_KEY"}}}}
}
func completeBody() []byte {
	return []byte(`{"choices":[{"message":{"role":"assistant","content":"fixture response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2}}`)
}

func TestModelTransportCommitsReceiptBeforeReturning(t *testing.T) {
	ledger := &modelLedger{}
	secrets := &fixtureSecrets{}
	received := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer fixture-provider-token-never-real" {
			t.Error("provider request lost path/auth")
			w.WriteHeader(400)
			return
		}
		var body map[string]any
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
			w.WriteHeader(400)
			return
		}
		received <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(completeBody())
	}))
	defer server.Close()
	worker := Worker{Store: ledger, Secrets: secrets}
	run := providerRun(server.URL)
	msg, e := worker.CompleteModel(context.Background(), run, "fixture-run/dsh/4", []Message{{Role: "user", Content: "synthetic input"}}, nil)
	if e != nil || msg.Content != "fixture response" || ledger.begins != 1 || ledger.ends != 1 || ledger.completedError != nil || ledger.attempt != "fixture-attempt" || ledger.operation != "fixture-run/dsh/4" || ledger.run.Fence != 7 || secrets.calls != 1 {
		t.Fatal("receipt ordering/identity mismatch", e)
	}
	body := <-received
	if body["model"] != "synthetic-provider" || body["max_tokens"] != float64(64) || body["stream"] != false {
		t.Fatal("provider parameters changed")
	}
	if !json.Valid(ledger.response) {
		t.Fatal("committed invalid transport body")
	}
}

func TestCachedModelAndRejectedIntentDoNotContactProvider(t *testing.T) {
	for _, mode := range []string{"cached", "stale-fence"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(500) }))
			defer server.Close()
			l := &modelLedger{}
			if mode == "cached" {
				l.cached = completeBody()
			} else {
				l.beginErr = ErrFence
			}
			secrets := &fixtureSecrets{}
			worker := Worker{Store: l, Secrets: secrets}
			msg, e := worker.CompleteModel(context.Background(), providerRun(server.URL), "fixture/op", nil, nil)
			if mode == "cached" && (e != nil || msg.Content != "fixture response") {
				t.Fatal(e)
			}
			if mode == "stale-fence" && !errors.Is(e, ErrFence) {
				t.Fatal(e)
			}
			if requests.Load() != 0 || secrets.calls != 0 || l.ends != 0 {
				t.Fatal("cached/rejected intent made external request")
			}
		})
	}
}

func TestProviderFailuresAndUnacknowledgedResultsAreNotSuccess(t *testing.T) {
	for _, mode := range []string{"http-error", "non-json", "truncated", "oversized", "receipt-fence", "secret-unavailable"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				switch mode {
				case "http-error":
					w.WriteHeader(502)
					_, _ = w.Write([]byte(`{"error":"fixture"}`))
				case "non-json":
					_, _ = w.Write([]byte("incomplete fixture"))
				case "truncated":
					_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"partial"},"finish_reason":"length"}]}`))
				case "oversized":
					_, _ = w.Write([]byte(strings.Repeat("x", (4<<20)+2)))
				default:
					_, _ = w.Write(completeBody())
				}
			}))
			defer server.Close()
			l := &modelLedger{}
			secrets := &fixtureSecrets{}
			if mode == "receipt-fence" {
				l.endErr = ErrFence
			}
			if mode == "secret-unavailable" {
				secrets.err = errors.New("fixture unavailable")
			}
			worker := Worker{Store: l, Secrets: secrets}
			msg, e := worker.CompleteModel(context.Background(), providerRun(server.URL), "fixture/op", nil, nil)
			if e == nil || msg.Content != "" || l.ends != 1 {
				t.Fatal("unacknowledged or failed result accepted", e)
			}
			if mode == "receipt-fence" {
				if !errors.Is(e, ErrFence) {
					t.Fatal(e)
				}
			} else if l.completedError == nil {
				t.Fatal("failed attempt not recorded")
			}
			if mode == "secret-unavailable" && requests.Load() != 0 {
				t.Fatal("request sent without credential")
			}
			if !json.Valid(l.response) {
				t.Fatal("ledger received invalid JSON")
			}
		})
	}
}

func TestToolTransportKeepsUnknownFenceAndRedirectBoundaries(t *testing.T) {	for _, status := range []int{409, 412, 307} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var redirected atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
			defer target.Close()
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/invoke" || r.Header.Get("Authorization") != "Bearer fixture-token" {
					t.Error("bad tool transport identity")
				}
				w.Header().Set("Location", target.URL)
				w.WriteHeader(status)
			}))
			defer peer.Close()
			worker := Worker{ToolHubURL: peer.URL, Token: "fixture-token"}
			_, e := worker.InvokeTool(context.Background(), Invocation{RunID: "fixture", OperationID: "fixture/tool/1"})
			if e == nil {
				t.Fatal("error boundary accepted")
			}
			if status == 409 && !errors.Is(e, ErrUnknown) {
				t.Fatal(e)
			}
			if status == 412 && !errors.Is(e, ErrFence) {
				t.Fatal(e)
			}
			if redirected.Load() != 0 {
				t.Fatal("service credential followed redirect")
			}
		})
	}
}
func TestToolTransportRetries503WithSameInvocation(t *testing.T) {
	var calls atomic.Int32
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got map[string]any
		if json.NewDecoder(r.Body).Decode(&got) != nil || got["operation_id"] != "fixture/tool/1" {
			t.Error("bad retried invocation")
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer peer.Close()
	worker := Worker{ToolHubURL: peer.URL, Token: "fixture-token"}
	body, e := worker.InvokeTool(context.Background(), Invocation{RunID: "fixture", OperationID: "fixture/tool/1"})
	if e != nil || string(body) != `{"ok":true}` || calls.Load() != 2 {
		t.Fatal("503 was not retried safely", e, string(body), calls.Load())
	}
}
func TestRuntimeUnavailableOnly503(t *testing.T) {
	if !runtimeUnavailable(&serviceResponseError{code: 503}) {
		t.Fatal("503 must be requeueable")
	}
	if runtimeUnavailable(&serviceResponseError{code: 400}) {
		t.Fatal("400 must remain a definite failure")
	}
	if runtimeUnavailable(errors.New("transport")) {
		t.Fatal("plain transport error must not be silently requeued")
	}
}
