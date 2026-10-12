package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/euler-gaozl/agent-os-sdk/contracts"
	"github.com/euler-gaozl/agent-os-sdk/servicehttp"
	"github.com/euler-gaozl/agent-os-sdk/toolcontract"
	"io"
	"net/http"
)

var ToolDefinitions = toolcontract.ToolDefinitions
var validateEndpoint = contracts.ValidateEndpoint
var externalHTTP = servicehttp.Client

type SecretReader interface {
	Secret(context.Context, string) (string, error)
}

func (w *Worker) Secret(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	if w.Secrets == nil {
		return "", errors.New("credential service required")
	}
	return w.Secrets.Secret(ctx, name)
}
func (w *Worker) PutArtifact(ctx context.Context, b []byte, media, root string) (string, error) {
	if w.Archives == nil {
		return "", errors.New("artifact service required")
	}
	return w.Archives.PutArtifact(ctx, b, media, root)
}
func (w *Worker) InvokeTool(ctx context.Context, inv Invocation) ([]byte, error) {
	return internalRequest(ctx, w.ToolHubURL+"/invoke", w.Token, inv)
}
func internalRequest(ctx context.Context, endpoint, token string, v any) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(JSON(v)))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, e := externalHTTP.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	body, e := io.ReadAll(io.LimitReader(resp.Body, 4<<20+1))
	if e != nil {
		return nil, e
	}
	if len(body) > 4<<20 {
		return nil, errors.New("internal response too large")
	}
	if resp.StatusCode == 409 {
		return nil, ErrUnknown
	}
	if resp.StatusCode == 412 {
		return nil, ErrFence
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("service HTTP %d: %s", resp.StatusCode, truncateUTF8(string(body), 500))
	}
	return body, nil
}
