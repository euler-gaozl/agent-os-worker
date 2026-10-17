package worker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGuardedHarnessIsSeparatelyPinnedAndOptIn(t *testing.T) {
	entry := sourceArtifactVersion(t, EngineDSHGuarded)
	handle, err := VerifyGuardedHarness(entry)
	if err != nil {
		t.Fatal(err)
	}
	testSourceArtifactTamper(t, EngineDSHGuarded)
	w := Worker{GuardedHarness: handle}
	if _, err = w.sourceForEngine(EngineDSHGuarded); err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{EngineDSHSource, EngineDSHResident, EngineDSHBudget, EngineDSHNative, EngineDSHFinal} {
		if _, err = verifySourceVersion(entry, old); err == nil {
			t.Fatal("historical pin accepted final runner", old)
		}
		if _, err = VerifyGuardedHarness(sourceArtifactVersion(t, old)); err == nil {
			t.Fatal("final pin accepted historical runner", old)
		}
	}
	w.GuardedHarness = &SourceHarness{entry: entry, engine: EngineDSHNative}
	if _, err = w.sourceForEngine(EngineDSHGuarded); err == nil {
		t.Fatal("swapped handle accepted")
	}
	r := nativeRun("https://fixture.invalid")
	r.Engine = EngineDSHGuarded
	if err = (&Worker{}).driveDSH(context.Background(), &r); err == nil {
		t.Fatal("missing final artifact fell back")
	}
}

func TestGuardedWorkerUsesDurableNativeProviderTransport(t *testing.T) {
	calls := 0
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; _, _ = w.Write(completeBody()) }))
	defer s.Close()
	l := &modelLedger{}
	w := Worker{Store: l, Secrets: &fixtureSecrets{}, nativeClient: s.Client()}
	r := nativeRun(s.URL)
	r.Engine = EngineDSHGuarded
	if _, err := w.CompleteModel(context.Background(), r, "wrong", nil, nil); err == nil || calls != 0 {
		t.Fatal("final used legacy transport")
	}
	if _, err := w.CompleteNativeModel(context.Background(), r, r.ID+"/dsh/1", []Message{{Role: "user", Content: "fixture"}}, nil); err != nil || calls != 1 || l.ends != 1 {
		t.Fatal("final native transport failed", err)
	}
	l.cached = l.response
	if _, err := w.CompleteNativeModel(context.Background(), r, r.ID+"/dsh/1", []Message{{Role: "user", Content: "fixture"}}, nil); err != nil || calls != 1 {
		t.Fatal("replay repeated provider", err)
	}
}
