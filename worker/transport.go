package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/euler-gaozl/agent-os-sdk/contracts"
	"github.com/euler-gaozl/agent-os-sdk/servicehttp"
	"github.com/euler-gaozl/agent-os-sdk/toolcontract"
	"io"
	"net/http"
	"strings"
	"time"
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

// Runtime owns this transaction and the frozen read/rejection/receipt. The
// Worker transports it directly, without routing through generic ToolHub tools.
func (w *Worker) SourceState(ctx context.Context, inv Invocation) ([]byte, error) {
	if w.RuntimeURL == "" {
		return nil, errors.New("Runtime endpoint required for source state")
	}
	for attempt := 0; attempt < 3; attempt++ {
		requestContext, cancel := context.WithTimeout(ctx, 10*time.Second)
		body, err := internalRequest(requestContext, strings.TrimRight(w.RuntimeURL, "/")+"/v1/source-state", w.Token, inv)
		cancel()
		if err == nil && json.Valid(body) {
			return body, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Fencing/intent/auth errors are definite; retry only uncertain delivery
		// and server failures. Runtime freezes reads, rejections and successes by
		// operation ID, so the identical invocation is safe after a lost reply.
		var status *serviceResponseError
		if errors.Is(err, ErrFence) || errors.Is(err, ErrUnknown) || (errors.As(err, &status) && status.code < 500) {
			return nil, err
		}
		if attempt < 2 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
			}
		}
	}
	// Preserve an explicitly recoverable run when the receipt is unreachable.
	// A transport failure is not proof that the atomic state effect did not run.
	return nil, fmt.Errorf("%w: source-state receipt unavailable after bounded retry", ErrUnknown)
}

type serviceResponseError struct {
	code   int
	detail string
}

func (e *serviceResponseError) Error() string {
	return fmt.Sprintf("service HTTP %d: %s", e.code, e.detail)
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
		return nil, &serviceResponseError{resp.StatusCode, truncateUTF8(string(body), 500)}
	}
	return body, nil
}
