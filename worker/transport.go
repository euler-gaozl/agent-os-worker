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
type sourceEngineContext struct{}

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
	// ToolHub returns 503 while Runtime is restarting and its local tool
	// dispatcher is unavailable. The operation ID/fence makes the same
	// invocation safe to retry; definite fence/unknown/auth errors do not retry.
	var last error
	for attempt := 0; attempt < 10; attempt++ {
		body, err := internalRequest(ctx, w.ToolHubURL+"/invoke", w.Token, inv)
		if err == nil {
			return body, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var status *serviceResponseError
		if errors.Is(err, ErrFence) || errors.Is(err, ErrUnknown) || (errors.As(err, &status) && status.code < 500) {
			return nil, err
		}
		last = err
		if attempt < 9 {
			delay := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 3 * time.Second, 3 * time.Second, 3 * time.Second, 3 * time.Second, 3 * time.Second}[attempt]
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return nil, last
}

// Runtime owns this transaction and the frozen read/rejection/receipt. The
// Worker transports it directly, without routing through generic ToolHub tools.
func (w *Worker) SourceState(ctx context.Context, inv Invocation) ([]byte, error) {
	if w.RuntimeURL == "" {
		return nil, errors.New("Runtime endpoint required for source state")
	}
	limit := int64(4 << 20)
	if engine, _ := ctx.Value(sourceEngineContext{}).(string); isNativeEngine(engine) {
		limit = 64 << 20
	}
	for attempt := 0; attempt < 6; attempt++ {
		requestContext, cancel := context.WithTimeout(ctx, 10*time.Second)
		body, err := internalRequestLimit(requestContext, strings.TrimRight(w.RuntimeURL, "/")+"/v1/source-state", w.Token, inv, limit)
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
		if attempt < 5 {
			delay := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 3 * time.Second}[attempt]
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
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
	return internalRequestLimit(ctx, endpoint, token, v, 4<<20)
}
func internalRequestLimit(ctx context.Context, endpoint, token string, v any, limit int64) ([]byte, error) {
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
	body, e := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if e != nil {
		return nil, e
	}
	if int64(len(body)) > limit {
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
