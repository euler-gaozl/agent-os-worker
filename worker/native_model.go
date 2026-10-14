package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/euler-gaozl/agent-os-runtime/execution"
)

// Native generation can exceed the historical 90-second whole-call timeout.
// Keep connection/header bounds and a per-response idle timer, plus the run's
// cancellation/fence context. Progress is bounded by a response byte limit.
var nativeHTTP = func() *http.Client {
	client := *externalHTTP
	transport := externalHTTP.Transport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 2 * time.Minute
	client.Transport = transport
	client.Timeout = 0
	return &client
}()

type nativeProgressReader struct {
	r     io.Reader
	timer *time.Timer
	idle  time.Duration
}

func (r nativeProgressReader) Read(p []byte) (int, error) {
	n, e := r.r.Read(p)
	if n > 0 {
		r.timer.Reset(r.idle)
	}
	return n, e
}

// This method only transports a frozen intent and records the provider result.
// Runtime independently derives and freezes the format-2 Harness outcome.
func (w *Worker) CompleteNativeModel(ctx context.Context, r Run, id string, messages []Message, tools []Tool) (json.RawMessage, error) {
	if r.Engine != EngineDSHNative {
		return nil, errors.New("native model protocol requires native engine")
	}
	cfg := r.Snapshot.Agent.Model
	if e := validateEndpoint(cfg.BaseURL); e != nil {
		return nil, e
	}
	request := execution.NativeModelRequest(cfg, messages, tools)
	if e := execution.InspectNativeRequest(request, r.Snapshot); e != nil {
		return nil, e
	}
	attempt, cached, e := w.Store.BeginModel(ctx, r, id, request)
	if e != nil {
		return nil, e
	}
	raw := cached
	if cached == nil {
		receipt, callErr := w.requestNativeModel(ctx, cfg, request)
		raw = receipt
		if e = w.Store.EndModel(ctx, r, id, attempt, raw, callErr); e != nil {
			return nil, e
		}
		if callErr != nil {
			return nil, callErr
		}
	}
	receipt, e := execution.ReadNativeReceipt(raw)
	if e != nil {
		return nil, e
	}
	if e = validateNativeCompletion(receipt); e != nil {
		return nil, e
	}
	return request, nil
}

func (w *Worker) requestNativeModel(ctx context.Context, cfg ModelConfig, request []byte) ([]byte, error) {
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, e := http.NewRequestWithContext(requestCtx, http.MethodPost, strings.TrimRight(cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(request))
	if e != nil {
		return nil, errors.New("invalid native model endpoint")
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.KeyEnv != "" {
		key, err := w.Secret(ctx, cfg.KeyEnv)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := nativeHTTP
	if w.nativeClient != nil {
		client = w.nativeClient
	}
	// ResponseHeaderTimeout begins only after the request body is written.
	// Bound upload + first-response separately so a peer that stops consuming
	// a large request cannot keep the Worker lease alive forever.
	firstResponse := 2 * time.Minute
	if w.nativeFirstResponse > 0 {
		firstResponse = w.nativeFirstResponse
	}
	sendTimer := time.AfterFunc(firstResponse, cancel)
	resp, e := client.Do(req)
	sendTimer.Stop()
	if e != nil {
		return nil, errors.New("native model transport interrupted before complete response")
	}
	defer resp.Body.Close()
	if requestCtx.Err() != nil {
		return nil, errors.New("native model request send or first response timed out")
	}
	idle := 2 * time.Minute
	if w.nativeIdle > 0 {
		idle = w.nativeIdle
	}
	timer := time.AfterFunc(idle, cancel)
	defer timer.Stop()
	body, e := io.ReadAll(io.LimitReader(nativeProgressReader{resp.Body, timer, idle}, execution.NativeResponseBytes+1))
	if e != nil || requestCtx.Err() != nil {
		return nil, errors.New("native model response interrupted or idle timeout")
	}
	if len(body) > execution.NativeResponseBytes {
		return nil, errors.New("native model response exceeds transport capacity")
	}
	raw, e := execution.NativeReceipt(resp.StatusCode, body)
	if e != nil {
		return nil, e
	}
	receipt, e := execution.ReadNativeReceipt(raw)
	if e != nil {
		return nil, e
	}
	return raw, validateNativeCompletion(receipt)
}

func validateNativeCompletion(receipt execution.NativeProviderReceipt) error {
	if execution.NativeContextRejected(receipt.HTTPStatus, receipt.Body) {
		return nil
	}
	if receipt.HTTPStatus < 200 || receipt.HTTPStatus >= 300 {
		return fmt.Errorf("model HTTP %d", receipt.HTTPStatus)
	}
	var completion Completion
	if json.Unmarshal(receipt.Body, &completion) != nil || len(completion.Choices) != 1 || !Has([]string{"stop", "tool_calls"}, completion.Choices[0].Finish) {
		return errors.New("native model output is incomplete or truncated")
	}
	m := completion.Choices[0].Message
	if m.Role != "assistant" {
		return errors.New("invalid native model message role")
	}
	seen := map[string]bool{}
	for _, call := range m.ToolCalls {
		if call.ID == "" || seen[call.ID] || call.Type != "function" || !json.Valid([]byte(call.Function.Arguments)) {
			return errors.New("invalid native model tool plan")
		}
		seen[call.ID] = true
	}
	if len(m.ToolCalls) > 8 {
		return errors.New("native tool plan exceeds 8 calls")
	}
	return nil
}
