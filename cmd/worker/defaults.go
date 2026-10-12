package main

import "os"

func applyLocalDefaults() error {
	for key, value := range map[string]string{"RUNTIME_URL": "http://127.0.0.1:8094", "REGISTRY_URL": "http://127.0.0.1:8096", "TOOLHUB_URL": "http://127.0.0.1:8091", "STORAGE_URL": "http://127.0.0.1:8092"} {
		if os.Getenv(key) == "" {
			if e := os.Setenv(key, value); e != nil {
				return e
			}
		}
	}
	return nil
}
