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
	servicehost.Main(servicehost.Options{Role: "worker", Port: "0", Database: true, Concurrency: 16, Authorize: worker.Authorize, Readiness: execution.Readiness}, func(ctx context.Context, db *pgxpool.Pool, c servicehttp.Config) (http.Handler, func(context.Context) error, error) {
		if e := applyLocalDefaults(); e != nil {
			return nil, nil, e
		}
		client := servicehost.Client()
		archives := artifactWriter{client, os.Getenv("STORAGE_URL")}
		var source, resident *worker.SourceHarness
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
		store, err := execution.NewSourceVersions(db, archives, versions)
		if err != nil {
			return nil, nil, err
		}
		w := &worker.Worker{Store: store, SourceHarness: source, ResidentHarness: resident, RuntimeURL: os.Getenv("RUNTIME_URL"), Archives: archives, Secrets: client, ID: servicehost.Env("INSTANCE_ID", "worker-"+contracts.ID()[:8]), Token: c.Token, ToolHubURL: client.CatalogURL}
		return servicehttp.New(c, db.Ping, func(*http.ServeMux) {}), w.Run, nil
	})
}
