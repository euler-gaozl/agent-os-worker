package worker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSourceVersionsCannotSwapPinsOrVerifiedHandles(t *testing.T) {
	v1 := sourceArtifactVersion(t, EngineDSHSource)
	v2 := sourceArtifactVersion(t, EngineDSHResident)
	if _, err := VerifySourceHarness(v2); err == nil {
		t.Fatal("v2 authorized by v1 pin")
	}
	if _, err := VerifyResidentHarness(v1); err == nil {
		t.Fatal("v1 authorized by v2 pin")
	}
	source, err := VerifySourceHarness(v1)
	if err != nil {
		t.Fatal(err)
	}
	resident, err := VerifyResidentHarness(v2)
	if err != nil {
		t.Fatal(err)
	}
	w := Worker{SourceHarness: source, ResidentHarness: resident}
	for _, version := range []string{EngineDSHSource, EngineDSHResident} {
		selected, err := w.sourceForEngine(version)
		if err != nil || selected.engine != version {
			t.Fatal("wrong version selected", version, err)
		}
	}
	w.SourceHarness, w.ResidentHarness = resident, source
	for _, version := range []string{EngineDSHSource, EngineDSHResident} {
		if _, err := w.sourceForEngine(version); err == nil {
			t.Fatal("swapped verified handle accepted", version)
		}
	}
	if _, err := w.sourceForEngine("future-source"); err == nil {
		t.Fatal("unknown source engine accepted")
	}
}

func TestSourceVersionsRecheckArtifactsBeforeProcessLaunch(t *testing.T) {
	for _, version := range []string{EngineDSHSource, EngineDSHResident} {
		t.Run(version, func(t *testing.T) {
			original := sourceArtifactVersion(t, version)
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "dist"), 0700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"source-runner.mjs", "dist/kernel.mjs", "dist/manifest.json", "dist/THIRD_PARTY_NOTICES.txt"} {
				body, err := os.ReadFile(filepath.Join(filepath.Dir(original), name))
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(root, name), body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			handle, err := verifySourceVersion(filepath.Join(root, "source-runner.mjs"), version)
			if err != nil {
				t.Fatal(err)
			}
			w := Worker{}
			if version == EngineDSHSource {
				w.SourceHarness = handle
			} else {
				w.ResidentHarness = handle
			}
			if err = os.WriteFile(filepath.Join(root, "dist/kernel.mjs"), []byte("tampered after verification"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = w.sourceForEngine(version); err == nil {
				t.Fatal("launch reused a stale verification")
			}
		})
	}
}
