package eval

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"maps"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/open-feature/go-sdk/openfeature"

	"runsten/internal/flags"
)

// provider answers each evaluation with answer, after recording its context. With ready
// set, it is not ready until the channel is closed; with initErr, it fails to start.
type provider struct {
	openfeature.NoopProvider
	answer  func(ctx context.Context, flat openfeature.FlattenedContext) openfeature.BoolResolutionDetail
	ready   chan struct{}
	initErr error

	mu   sync.Mutex
	seen []openfeature.FlattenedContext
}

func (p *provider) BooleanEvaluation(ctx context.Context, _ string, _ bool, flat openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
	p.mu.Lock()
	p.seen = append(p.seen, flat)
	p.mu.Unlock()
	return p.answer(ctx, flat)
}

func (p *provider) Init(openfeature.EvaluationContext) error {
	if p.ready != nil {
		<-p.ready
	}
	return p.initErr
}

func (p *provider) Shutdown() {}

func (p *provider) contexts() []openfeature.FlattenedContext {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.seen)
}

func answer(v bool) openfeature.BoolResolutionDetail {
	return openfeature.BoolResolutionDetail{Value: v, ProviderResolutionDetail: openfeature.ProviderResolutionDetail{Reason: openfeature.TargetingMatchReason}}
}

func newEvaluator(t *testing.T) (*Evaluator, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	e := New(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { _ = e.Shutdown(context.Background()) }) //nolint:usetesting // t.Context is canceled by then
	return e, &logs
}

// waitReady waits for the provider's initialization, which SetProvider runs apart.
func waitReady(t *testing.T, e *Evaluator) {
	t.Helper()
	for e.client.State() != openfeature.ReadyState {
		select {
		case <-t.Context().Done():
			t.Fatal("provider never ready")
		default:
			runtime.Gosched()
		}
	}
}

// TestDefaults: without a provider, every flag has its declared default.
func TestDefaults(t *testing.T) {
	e, logs := newEvaluator(t)
	for _, f := range flags.All() {
		if got := e.Enabled(t.Context(), f.Name, "account-1"); got != f.Default {
			t.Errorf("%s = %v, want its default %v", f.Name, got, f.Default)
		}
	}
	if e.Enabled(t.Context(), "source.undeclared", "") || !strings.Contains(logs.String(), "flag not declared") {
		t.Errorf("an undeclared flag is off, and logged: %s", logs)
	}
}

// TestEnv: RUNSTEN_FLAGS sets a flag for every account; one it does not name keeps its
// default, without a warning.
func TestEnv(t *testing.T) {
	e, logs := newEvaluator(t)
	values, _, err := flags.Parse("source.tesla=false")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Use(t.Context(), Env(values), ""); err != nil {
		t.Fatal(err)
	}
	for _, account := range []string{"", "account-1", "account-2"} {
		if e.Enabled(t.Context(), flags.SourceTesla, account) {
			t.Errorf("source.tesla on for %q", account)
		}
	}

	e2, _ := newEvaluator(t)
	if err := e2.Use(t.Context(), Env(nil), ""); err != nil {
		t.Fatal(err)
	}
	if !e2.Enabled(t.Context(), flags.SourceTesla, "account-1") {
		t.Error("source.tesla unset: its default, on")
	}
	if logs.Len() != 0 {
		t.Errorf("logged: %s", logs)
	}
}

// TestContext: the provider is given the account's ID and the instance, nothing more.
func TestContext(t *testing.T) {
	e, _ := newEvaluator(t)
	p := &provider{answer: func(_ context.Context, flat openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
		return answer(flat[openfeature.TargetingKey] == "account-beta")
	}}
	// As a downstream build does: its provider in place of RUNSTEN_FLAGS's.
	if err := e.Use(t.Context(), Env(map[flags.Name]bool{flags.SourceTesla: false}), ""); err != nil {
		t.Fatal(err)
	}
	if err := e.Use(t.Context(), p, "staging"); err != nil {
		t.Fatal(err)
	}
	if !e.Enabled(t.Context(), flags.SourceTesla, "account-beta") || e.Enabled(t.Context(), flags.SourceTesla, "account-1") {
		t.Error("the provider targets the account")
	}
	e.Enabled(t.Context(), flags.SourceTesla, "")
	got := p.contexts()
	want := []map[string]any{
		{openfeature.TargetingKey: "account-beta", "instance": "staging"},
		{openfeature.TargetingKey: "account-1", "instance": "staging"},
		{"instance": "staging"},
	}
	if len(got) != len(want) {
		t.Fatalf("contexts = %v", got)
	}
	for i := range want {
		if !maps.Equal(got[i], want[i]) {
			t.Errorf("context %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestProviderAbsentSlowOrFailing: a provider not ready, failing to start, too slow or in
// error changes nothing: each flag has its default; a failure is logged once.
func TestProviderAbsentSlowOrFailing(t *testing.T) {
	off := func(context.Context, openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
		return answer(false)
	}

	notReady, notReadyLogs := newEvaluator(t)
	notReady.readyWait = 0
	ready := make(chan struct{})
	if err := notReady.Use(t.Context(), &provider{answer: off, ready: ready}, ""); err != nil {
		t.Fatal(err)
	}
	if !notReady.Enabled(t.Context(), flags.SourceTesla, "account-1") || !strings.Contains(notReadyLogs.String(), "not ready yet") {
		t.Errorf("not ready: the default, logged: %s", notReadyLogs)
	}
	close(ready)
	waitReady(t, notReady)
	if notReady.Enabled(t.Context(), flags.SourceTesla, "account-1") {
		t.Error("once ready: the provider's value")
	}

	broken, brokenLogs := newEvaluator(t)
	if err := broken.Use(t.Context(), &provider{answer: off, initErr: errors.New("no server")}, ""); err != nil {
		t.Fatal(err)
	}
	// Still asked, as OpenFeature asks a provider in error: it may answer from what it had.
	if !strings.Contains(brokenLogs.String(), "failed to start") {
		t.Errorf("failing to start: not logged: %s", brokenLogs)
	}

	slow, _ := newEvaluator(t)
	if err := slow.Use(t.Context(), &provider{answer: func(ctx context.Context, _ openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
		<-ctx.Done() // past Timeout
		return openfeature.BoolResolutionDetail{
			Value:                    false,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{ResolutionError: openfeature.NewGeneralResolutionError(ctx.Err().Error())},
		}
	}}, ""); err != nil {
		t.Fatal(err)
	}
	if !slow.Enabled(t.Context(), flags.SourceTesla, "account-1") {
		t.Error("too slow: the default")
	}

	failing, logs := newEvaluator(t)
	if err := failing.Use(t.Context(), &provider{answer: func(context.Context, openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
		return openfeature.BoolResolutionDetail{
			Value:                    false,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{ResolutionError: openfeature.NewGeneralResolutionError("unreachable")},
		}
	}}, ""); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if !failing.Enabled(t.Context(), flags.SourceTesla, "account-1") {
			t.Error("failing: the default")
		}
	}
	if n := strings.Count(logs.String(), "flag not evaluated"); n != 1 {
		t.Errorf("failing: logged %d times, want once: %s", n, logs)
	}
}
