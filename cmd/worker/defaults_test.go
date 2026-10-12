package main

import (
	"os"
	"testing"
)

func TestLocalDefaultsAndExplicitEndpoints(t *testing.T) {
	expected := map[string]string{"RUNTIME_URL": "http://127.0.0.1:8094", "REGISTRY_URL": "http://127.0.0.1:8096", "TOOLHUB_URL": "http://127.0.0.1:8091", "STORAGE_URL": "http://127.0.0.1:8092"}
	for k := range expected {
		t.Setenv(k, "")
	}
	if e := applyLocalDefaults(); e != nil {
		t.Fatal(e)
	}
	for k, v := range expected {
		if os.Getenv(k) != v {
			t.Fatal(k)
		}
	}
	for k := range expected {
		t.Setenv(k, "https://explicit.invalid/"+k)
	}
	if e := applyLocalDefaults(); e != nil {
		t.Fatal(e)
	}
	for k := range expected {
		if os.Getenv(k) != "https://explicit.invalid/"+k {
			t.Fatal("explicit endpoint replaced", k)
		}
	}
}
