package worker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func sourceArtifact(t *testing.T) string {
	return sourceArtifactVersion(t, EngineDSHSource)
}

func sourceArtifactVersion(t *testing.T, version string) string {
	t.Helper()
	entry := os.Getenv("TEST_SOURCE_DSH_RUNNER")
	if version == EngineDSHResident {
		entry = os.Getenv("TEST_RESIDENT_DSH_RUNNER")
	}
	if version == EngineDSHBudget {
		entry = os.Getenv("TEST_BUDGET_DSH_RUNNER")
	}
	if version == EngineDSHNative {
		entry = os.Getenv("TEST_NATIVE_DSH_RUNNER")
	}
	if version == EngineDSHFinal {
		entry = os.Getenv("TEST_FINAL_DSH_RUNNER")
	}
	if version == EngineDSHGuarded {
		entry = os.Getenv("TEST_GUARDED_DSH_RUNNER")
	}
	if entry == "" {
		if os.Getenv("AGENTOS_REQUIRE_SOURCE") == "1" {
			t.Fatal("pinned source artifact required")
		}
		t.Skip("source artifact not configured")
	}
	if _, err := verifySourceVersion(entry, version); err != nil {
		t.Fatal(err)
	}
	return entry
}

func TestSourceArtifactCompiledPinRejectsTamper(t *testing.T) {
	for _, version := range []string{EngineDSHSource, EngineDSHResident} {
		t.Run(version, func(t *testing.T) { testSourceArtifactTamper(t, version) })
	}
}

func testSourceArtifactTamper(t *testing.T, version string) {
	entry := sourceArtifactVersion(t, version)
	for _, bad := range []string{"source-runner.mjs", "dist/kernel.mjs", "dist/manifest.json", "dist/THIRD_PARTY_NOTICES.txt"} {
		t.Run(bad, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "dist"), 0700); err != nil {
				t.Fatal(err)
			}
			for _, p := range []string{"source-runner.mjs", "dist/kernel.mjs", "dist/manifest.json", "dist/THIRD_PARTY_NOTICES.txt"} {
				body, err := os.ReadFile(filepath.Join(filepath.Dir(entry), p))
				if err != nil {
					t.Fatal(err)
				}
				if p == bad {
					body = append(body, ' ')
				}
				if err = os.WriteFile(filepath.Join(root, p), body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := verifySourceVersion(filepath.Join(root, "source-runner.mjs"), version); err == nil {
				t.Fatal("modified artifact accepted")
			}
		})
	}
	if _, err := verifySourceVersion("source-runner.mjs", version); err == nil {
		t.Fatal("relative artifact accepted")
	}
}

func TestSourceEngineDoesNotFallBackToLegacyRunner(t *testing.T) {
	for _, version := range []string{EngineDSHSource, EngineDSHResident} {
		r := providerRun("https://fixture.invalid")
		r.Engine = version
		t.Setenv("DSH_RUNNER", "legacy-must-not-be-started.mjs")
		if err := (&Worker{}).driveDSH(context.Background(), &r); err == nil || !strings.Contains(err.Error(), "not enabled") {
			t.Fatal("source was not opt-in", err)
		}
	}
}

func TestSourceStateUsesWorkerIdentityAndPreservesFrozenRejection(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var inv Invocation
		if r.URL.Path != "/v1/source-state" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer fixture-worker" || json.NewDecoder(r.Body).Decode(&inv) != nil || inv.Fence != 9 || inv.OperationID != "fixture/working-state/7" {
			t.Error("source invocation identity changed")
			w.WriteHeader(400)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rejected":true,"status":409,"error":"fixture CAS"}`))
	}))
	defer server.Close()
	w := &Worker{RuntimeURL: server.URL + "/", Token: "fixture-worker", ToolHubURL: "http://must-not-call.invalid"}
	body, err := w.SourceState(context.Background(), Invocation{RunID: "fixture", OperationID: "fixture/working-state/7", Owner: "fixture-owner", Fence: 9, Name: "working_state_update", Arguments: json.RawMessage(`{}`)})
	if err != nil || calls.Load() != 1 || !strings.Contains(string(body), `"rejected":true`) {
		t.Fatal("frozen rejection transport changed", err)
	}
	if _, err = (&Worker{}).SourceState(context.Background(), Invocation{}); err == nil {
		t.Fatal("missing Runtime endpoint accepted")
	}
}

func TestSourceStateRetriesUncertainReplyWithSameInvocation(t *testing.T) {
	for _, mode := range []string{"lost-reply", "unreachable", "unauthorized"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			var first []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var inv Invocation
				if json.NewDecoder(r.Body).Decode(&inv) != nil {
					t.Error("bad invocation")
				}
				body := JSON(inv)
				n := calls.Add(1)
				if n == 1 {
					first = body
				} else if string(body) != string(first) {
					t.Error("retry changed invocation")
				}
				if mode == "unauthorized" {
					w.WriteHeader(401)
					return
				}
				if mode == "unreachable" {
					w.WriteHeader(503)
					return
				}
				if n == 1 {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
					return
				}
				_, _ = w.Write([]byte(`{"revision":1}`))
			}))
			defer server.Close()
			w := Worker{RuntimeURL: server.URL}
			_, err := w.SourceState(context.Background(), Invocation{RunID: "fixture", Owner: "w", Fence: 1, OperationID: "fixture/working-state/7", Name: "working_state_read", Arguments: json.RawMessage(`{}`)})
			switch mode {
			case "lost-reply":
				if err != nil || calls.Load() != 2 {
					t.Fatal("lost reply not recovered", err, calls.Load())
				}
			case "unreachable":
				if !errors.Is(err, ErrUnknown) || calls.Load() != 3 {
					t.Fatal("unavailable receipt not retained as unknown", err, calls.Load())
				}
			case "unauthorized":
				if err == nil || calls.Load() != 1 {
					t.Fatal("definite authentication error retried", err, calls.Load())
				}
			}
		})
	}
}

func TestSourceBudgetCountsFinalConstraintsToolsAndOutputBeforeIntent(t *testing.T) {
	for _, version := range []string{EngineDSHSource, EngineDSHResident} {
		t.Run(version, func(t *testing.T) { testSourceBudget(t, version) })
	}
}

func testSourceBudget(t *testing.T, version string) {
	for _, part := range []string{"constraints", "tools", "output", "fits"} {
		t.Run(part, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); _, _ = w.Write(completeBody()) }))
			defer server.Close()
			ledger := &modelLedger{}
			w := &Worker{Store: ledger, Secrets: &fixtureSecrets{}}
			run := providerRun(server.URL)
			run.Engine = version
			run.Snapshot.Agent.ContextTokens = 8192
			messages := []Message{{Role: "user", Content: "data"}}
			var tools []Tool
			switch part {
			case "constraints":
				messages = append([]Message{{Role: "system", Content: "Current platform constraints: " + strings.Repeat("x", 32768)}}, messages...)
			case "tools":
				tools = []Tool{{Name: "large_schema", Description: strings.Repeat("x", 32768)}}
			case "output":
				run.Snapshot.Agent.Model.MaxTokens = 8192
			}
			_, err := w.CompleteModel(context.Background(), run, "fixture/model", messages, tools)
			if part == "fits" {
				if err != nil || calls.Load() != 1 || ledger.begins != 1 {
					t.Fatal("fitting source request rejected", err)
				}
			} else if err == nil || calls.Load() != 0 || ledger.begins != 0 {
				t.Fatal("oversized final request reached model intent/provider", err)
			}
		})
	}
}
