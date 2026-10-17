package worker

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/euler-gaozl/agent-os-runtime/execution"
)

const EngineDSHSource = execution.EngineDSHSource
const EngineDSHResident = execution.EngineDSHResident
const EngineDSHBudget = execution.EngineDSHBudget
const EngineDSHNative = execution.EngineDSHNative
const EngineDSHFinal = execution.EngineDSHFinal
const EngineDSHGuarded = execution.EngineDSHGuarded
const EngineDSHGuardedV6 = execution.EngineDSHGuardedV6

func isNativeEngine(engine string) bool {
	return engine == EngineDSHNative || execution.IsFinalEngine(engine)
}

func isSourceEngine(engine string) bool { return execution.IsSourceEngine(engine) }

// The compiled lock is the trust anchor. An adjacent, editable manifest alone
// must never authorize a different runner for the same replay engine.
//
//go:embed source-harness-lock.json
var sourceHarnessLock []byte

//go:embed resident-harness-lock.json
var residentHarnessLock []byte

//go:embed budget-harness-lock.json
var budgetHarnessLock []byte

//go:embed native-harness-lock.json
var nativeHarnessLock []byte

//go:embed final-harness-lock.json
var finalHarnessLock []byte

//go:embed guarded-harness-lock.json
var guardedHarnessLock []byte

type harnessLock struct {
	Engine   string `json:"engine"`
	Upstream string `json:"upstream_commit"`
	Manifest string `json:"manifest_sha256"`
}

// SourceHarness is constructed only after checking the compiled release pin.
// Deploy this artifact read-only; verification is integrity checking, not an OS sandbox.
type SourceHarness struct{ entry, engine string }

func VerifySourceHarness(entry string) (*SourceHarness, error) {
	return verifySourceVersion(entry, EngineDSHSource)
}

func VerifyResidentHarness(entry string) (*SourceHarness, error) {
	return verifySourceVersion(entry, EngineDSHResident)
}

func VerifyBudgetHarness(entry string) (*SourceHarness, error) {
	return verifySourceVersion(entry, EngineDSHBudget)
}
func VerifyNativeHarness(entry string) (*SourceHarness, error) {
	return verifySourceVersion(entry, EngineDSHNative)
}
func VerifyFinalHarness(entry string) (*SourceHarness, error) {
	return verifySourceVersion(entry, EngineDSHFinal)
}

func VerifyGuardedHarness(entry string) (*SourceHarness, error) {
	return verifySourceVersion(entry, EngineDSHGuarded)
}

func verifySourceVersion(entry, engine string) (*SourceHarness, error) {
	var pin []byte
	switch engine {
	case EngineDSHSource:
		pin = sourceHarnessLock
	case EngineDSHResident:
		pin = residentHarnessLock
	case EngineDSHBudget:
		pin = budgetHarnessLock
	case EngineDSHNative:
		pin = nativeHarnessLock
	case EngineDSHFinal:
		pin = finalHarnessLock
	case EngineDSHGuarded:
		pin = guardedHarnessLock
	case EngineDSHGuardedV6:
		return nil, errors.New("guarded-v6 runner is not packaged; only terminal history may remain")
	default:
		return nil, errors.New("unsupported source artifact engine")
	}
	var lock harnessLock
	if err := json.Unmarshal(pin, &lock); err != nil {
		return nil, err
	}
	if lock.Engine != engine {
		return nil, errors.New("compiled source engine mismatch")
	}
	return verifySourceHarness(entry, lock)
}

func verifySourceHarness(entry string, lock harnessLock) (*SourceHarness, error) {
	if !filepath.IsAbs(entry) || filepath.Base(entry) != "source-runner.mjs" || !isSourceEngine(lock.Engine) {
		return nil, errors.New("absolute pinned source-runner.mjs required")
	}
	root := filepath.Dir(entry)
	for _, dir := range []string{root, filepath.Join(root, "dist")} {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("source artifact directory unavailable or linked")
		}
	}
	body, err := checkedSourceFile(filepath.Join(root, "dist", "manifest.json"), lock.Manifest, 256<<10)
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Format   int    `json:"format"`
		Target   string `json:"target"`
		Engine   string `json:"source_engine"`
		Upstream string `json:"upstream_commit"`
		Runner   string `json:"source_runner_sha256"`
		Kernel   string `json:"kernel_sha256"`
		Notices  string `json:"notices_sha256"`
	}
	if json.Unmarshal(body, &manifest) != nil || manifest.Format != 1 || manifest.Target != "node24" || manifest.Engine != lock.Engine || manifest.Upstream != lock.Upstream {
		return nil, errors.New("source artifact protocol or provenance mismatch")
	}
	for _, file := range []struct {
		path, digest string
		limit        int64
	}{
		{entry, manifest.Runner, 256 << 10},
		{filepath.Join(root, "dist", "kernel.mjs"), manifest.Kernel, 16 << 20},
		{filepath.Join(root, "dist", "THIRD_PARTY_NOTICES.txt"), manifest.Notices, 256 << 10},
	} {
		if _, err = checkedSourceFile(file.path, file.digest, file.limit); err != nil {
			return nil, err
		}
	}
	return &SourceHarness{entry: filepath.Clean(entry), engine: lock.Engine}, nil
}

func (w *Worker) sourceForEngine(engine string) (*SourceHarness, error) {
	var selected *SourceHarness
	switch engine {
	case EngineDSHSource:
		selected = w.SourceHarness
	case EngineDSHResident:
		selected = w.ResidentHarness
	case EngineDSHBudget:
		selected = w.BudgetHarness
	case EngineDSHNative:
		selected = w.NativeHarness
	case EngineDSHFinal:
		selected = w.FinalHarness
	case EngineDSHGuarded:
		selected = w.GuardedHarness
	case EngineDSHGuardedV6:
		return nil, errors.New("guarded-v6 runner is not packaged")
	default:
		return nil, errors.New("unsupported source engine")
	}
	if selected == nil {
		return nil, errors.New("source engine is not enabled")
	}
	if selected.engine != engine {
		return nil, errors.New("source artifact belongs to a different engine")
	}
	// Recheck the exact compiled pin before every process. No version fallback.
	return verifySourceVersion(selected.entry, engine)
}

func checkedSourceFile(path, want string, limit int64) ([]byte, error) {
	if len(want) != 64 {
		return nil, errors.New("source digest missing")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("source artifact unavailable or oversized: %s", filepath.Base(path))
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(body)
	if int64(len(body)) > limit || hex.EncodeToString(digest[:]) != want {
		return nil, fmt.Errorf("source artifact digest mismatch: %s", filepath.Base(path))
	}
	return body, nil
}
