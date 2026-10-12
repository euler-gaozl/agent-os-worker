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
		w := &worker.Worker{Store: execution.New(db, archives), Archives: archives, Secrets: client, ID: servicehost.Env("INSTANCE_ID", "worker-"+contracts.ID()[:8]), Token: c.Token, ToolHubURL: client.CatalogURL}
		return servicehttp.New(c, db.Ping, func(*http.ServeMux) {}), w.Run, nil
	})
}
