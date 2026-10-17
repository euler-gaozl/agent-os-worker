package main

import (
	"context"
	"github.com/euler-gaozl/agent-os-runtime/execution"
	"github.com/euler-gaozl/agent-os-sdk/contracts"
	"github.com/euler-gaozl/agent-os-sdk/serviceclient"
	"github.com/euler-gaozl/agent-os-sdk/servicehost"
	"github.com/euler-gaozl/agent-os-sdk/servicehttp"
	"github.com/euler-gaozl/agent-os-worker/worker"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"os"
)

type artifactWriter struct {
	client serviceclient.Client
	url    string
}

func (a artifactWriter) PutArtifact(ctx context.Context, b []byte, media, root string) (string, error) {
	return a.client.PutArtifact(ctx, a.url, b, media, root)
}
func main() {
	if reportExecutionAccess(os.Args[1:], os.Stdout) {
		return
	}
	servicehost.Main(servicehost.Options{Role: "worker", Port: "0", Database: true, Concurrency: 16, Authorize: worker.Authorize, Readiness: execution.Readiness}, func(ctx context.Context, db *pgxpool.Pool, c servicehttp.Config) (http.Handler, func(context.Context) error, error) {
		if e := applyLocalDefaults(); e != nil {
			return nil, nil, e
		}
		client := servicehost.Client()
		archives := artifactWriter{client, os.Getenv("STORAGE_URL")}
		var source, resident, budget, native, final, guarded, guardedV6 *worker.SourceHarness
		var sourceV2, residentV2, budgetV2, nativeV2, finalV2, guardedV2 *worker.SourceHarness
		var versions []string
		if entry := os.Getenv("DSH_SOURCE_RUNNER"); entry != "" {
			var err error
			source, err = worker.VerifySourceHarness(entry)
			if err != nil {
				return nil, nil, err
			}
			versions = append(versions, worker.EngineDSHSource)
		}
		if entry := os.Getenv("DSH_RESIDENT_RUNNER"); entry != "" {
			var err error
			resident, err = worker.VerifyResidentHarness(entry)
			if err != nil {
				return nil, nil, err
			}
			versions = append(versions, worker.EngineDSHResident)
		}
		if entry := os.Getenv("DSH_BUDGET_RUNNER"); entry != "" {
			var err error
			budget, err = worker.VerifyBudgetHarness(entry)
			if err != nil {
				return nil, nil, err
			}
			versions = append(versions, worker.EngineDSHBudget)
		}
		if entry := os.Getenv("DSH_NATIVE_RUNNER"); entry != "" {
			var err error
			native, err = worker.VerifyNativeHarness(entry)
			if err != nil {
				return nil, nil, err
			}
			versions = append(versions, worker.EngineDSHNative)
		}
		if entry := os.Getenv("DSH_FINAL_RUNNER"); entry != "" {
			var err error
			final, err = worker.VerifyFinalHarness(entry)
			if err != nil {
				return nil, nil, err
			}
			versions = append(versions, worker.EngineDSHFinal)
		}
		if entry := os.Getenv("DSH_GUARDED_RUNNER"); entry != "" {
			var err error
			guarded, err = worker.VerifyGuardedHarness(entry)
			if err != nil {
				return nil, nil, err
			}
			versions = append(versions, worker.EngineDSHGuarded)
		}
		if entry := os.Getenv("DSH_GUARDED_V6_RUNNER"); entry != "" {
			var err error
			guardedV6, err = worker.VerifyGuardedV6Harness(entry)
			if err != nil {
				return nil, nil, err
			}
			versions = append(versions, worker.EngineDSHGuardedV6)
		}
		if entry := os.Getenv("DSH_SOURCE_V2_RUNNER"); entry != "" {
			var err error
			sourceV2, err = worker.VerifySourceVersion(entry, worker.EngineDSHSourceV2)
			if err != nil {
				return nil, nil, err
			}
			versions = append(versions, worker.EngineDSHSourceV2)
		}
		if entry := os.Getenv("DSH_RESIDENT_V2_RUNNER"); entry != "" {
			var err error
			residentV2, err = worker.VerifySourceVersion(entry, worker.EngineDSHResidentV2)
			if err != nil {
				return nil, nil, err
			}
			versions = append(versions, worker.EngineDSHResidentV2)
		}
		if entry := os.Getenv("DSH_BUDGET_V2_RUNNER"); entry != "" {
			var err error
			budgetV2, err = worker.VerifySourceVersion(entry, worker.EngineDSHBudgetV2)
			if err != nil {
				return nil, nil, err
			}
			versions = append(versions, worker.EngineDSHBudgetV2)
		}
		if entry := os.Getenv("DSH_NATIVE_V2_RUNNER"); entry != "" {
			var err error
			nativeV2, err = worker.VerifySourceVersion(entry, worker.EngineDSHNativeV2)
			if err != nil {
				return nil, nil, err
			}
			versions = append(versions, worker.EngineDSHNativeV2)
		}
		if entry := os.Getenv("DSH_FINAL_V2_RUNNER"); entry != "" {
			var err error
			finalV2, err = worker.VerifySourceVersion(entry, worker.EngineDSHFinalV2)
			if err != nil {
				return nil, nil, err
			}
			versions = append(versions, worker.EngineDSHFinalV2)
		}
		if entry := os.Getenv("DSH_GUARDED_V2_RUNNER"); entry != "" {
			var err error
			guardedV2, err = worker.VerifySourceVersion(entry, worker.EngineDSHGuardedV2)
			if err != nil {
				return nil, nil, err
			}
			versions = append(versions, worker.EngineDSHGuardedV2)
		}
		store, err := execution.NewSourceVersions(db, archives, versions)
		if err != nil {
			return nil, nil, err
		}
		w := &worker.Worker{Store: store, SourceHarness: source, ResidentHarness: resident, BudgetHarness: budget, NativeHarness: native, FinalHarness: final, GuardedHarness: guarded, GuardedV6Harness: guardedV6, SourceV2Harness: sourceV2, ResidentV2Harness: residentV2, BudgetV2Harness: budgetV2, NativeV2Harness: nativeV2, FinalV2Harness: finalV2, GuardedV2Harness: guardedV2, RuntimeURL: os.Getenv("RUNTIME_URL"), Archives: archives, Secrets: client, ID: servicehost.Env("INSTANCE_ID", "worker-"+contracts.ID()[:8]), Token: c.Token, ToolHubURL: client.CatalogURL}
		return servicehttp.New(c, db.Ping, func(*http.ServeMux) {}), w.Run, nil
	})
}
