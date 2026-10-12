package worker

import (
	"context"
	"encoding/json"
	"github.com/euler-gaozl/agent-os-runtime/execution"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type hostPersistence struct {
	Persistence
	exchange func(context.Context, *Run, execution.Frame, execution.Effects) (any, bool, error)
}

func (p hostPersistence) Exchange(c context.Context, r *Run, f execution.Frame, e execution.Effects) (any, bool, error) {
	return p.exchange(c, r, f, e)
}

func TestDSHSubprocessReceivesNoServiceOrProviderSecrets(t *testing.T) {
	node := os.Getenv("AGENTOS_NODE")
	if node == "" {
		var e error
		node, e = exec.LookPath("node")
		if e != nil {
			t.Skip("Node unavailable; subprocess host test not executed")
		}
	}
	for _, key := range []string{"SERVICE_TOKEN", "DATABASE_URL", "MASTER_KEY", "DEEPSEEK_API_KEY", "FIXTURE_PRIVATE_VALUE"} {
		t.Setenv(key, "fixture-only-sensitive")
	}
	entry := filepath.Join(t.TempDir(), "host.mjs")
	body := `import readline from 'node:readline';const input=readline.createInterface({input:process.stdin});input.once('line',line=>{const init=JSON.parse(line);const leaked=['SERVICE_TOKEN','DATABASE_URL','MASTER_KEY','DEEPSEEK_API_KEY','FIXTURE_PRIVATE_VALUE'].some(k=>k in process.env);console.log(JSON.stringify({id:1,method:'finish',params:{leaked,task:init.task,disabled:process.env.DSH_TELEMETRY_DISABLED,heapFlag:process.execArgv.includes('--max-old-space-size=192')},events:[],event_start:0,event_end:0}));});`
	if e := os.WriteFile(entry, []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("AGENTOS_NODE", node)
	t.Setenv("DSH_RUNNER", entry)
	calls := 0
	p := hostPersistence{exchange: func(_ context.Context, r *Run, f execution.Frame, e execution.Effects) (any, bool, error) {
		calls++
		var v struct {
			Leaked         bool
			Task, Disabled string
			HeapFlag       bool
		}
		if err := json.Unmarshal(f.Params, &v); err != nil || v.Leaked || v.Task != "fixture task" || v.Disabled != "1" || !v.HeapFlag {
			t.Fatal("subprocess environment/input boundary", err)
		}
		return map[string]bool{"done": true}, true, nil
	}}
	run := providerRun("https://fixture.invalid")
	run.Engine = EngineDSHEvidence
	run.Task = "fixture task"
	run.Snapshot.Agent.ContextTokens = 32000
	if e := (&Worker{Store: p}).driveDSH(context.Background(), &run); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal("missing exchange")
	}
}

func TestDSHRejectsOutOfOrderFrameBeforePersistence(t *testing.T) {
	node := os.Getenv("AGENTOS_NODE")
	if node == "" {
		var e error
		node, e = exec.LookPath("node")
		if e != nil {
			t.Skip("Node unavailable; protocol host test not executed")
		}
	}
	entry := filepath.Join(t.TempDir(), "wrong-sequence.mjs")
	if e := os.WriteFile(entry, []byte(`process.stdin.once('data',()=>console.log(JSON.stringify({id:2,method:'finish',params:{result:'invalid sequence'}})));`), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("AGENTOS_NODE", node)
	t.Setenv("DSH_RUNNER", entry)
	called := false
	p := hostPersistence{exchange: func(context.Context, *Run, execution.Frame, execution.Effects) (any, bool, error) {
		called = true
		return nil, false, nil
	}}
	run := providerRun("https://fixture.invalid")
	run.Engine = EngineDSHEvidence
	if e := (&Worker{Store: p}).driveDSH(context.Background(), &run); e == nil || !strings.Contains(e.Error(), "sequence mismatch") {
		t.Fatal("out-of-order frame accepted", e)
	}
	if called {
		t.Fatal("invalid sequence reached persistence")
	}
}
