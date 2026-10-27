// Package eval evaluates the core's flags through OpenFeature: the only package that
// knows its SDK. The binaries choose the provider: the core's, Env (RUNSTEN_FLAGS), or a
// downstream build's, through their extension point.
package eval

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/isolated"
	"github.com/open-feature/go-sdk/openfeature/memprovider"

	"runsten/internal/flags"
)

// Timeout bounds an evaluation for a provider that honors its context, at best: the SDK
// does not enforce it, and a provider that ignores it holds the request.
const Timeout = 200 * time.Millisecond

// ReadyWait is how long Use waits for a provider to be ready, so that a flag set off is
// off from the first request; beyond it, each flag has its default until it is.
const ReadyWait = 5 * time.Second

// instanceKey is the attribute of the evaluation context that names the instance.
const instanceKey = "instance"

// Evaluator evaluates the flags on an OpenFeature API of its own, never the SDK's global
// one. Without a provider, or while it is not ready, each flag has its default.
type Evaluator struct {
	api    *openfeature.EvaluationAPI
	client *openfeature.Client
	log    *slog.Logger
	// readyWait is ReadyWait, shorter in tests.
	readyWait time.Duration
	// failing is the latest failure logged of each flag, logged again only when it
	// changes: a provider down would log every request otherwise.
	failing sync.Map
}

// New returns an evaluator without a provider.
func New(log *slog.Logger) *Evaluator {
	api := isolated.NewAPI()
	return &Evaluator{api: api, client: api.NewClient(), log: log, readyWait: ReadyWait}
}

// Use makes p the provider, instance naming this instance in the evaluation context of
// every flag (empty: none, as for Env, which is the instance's own). It waits up to
// ReadyWait for p; a provider not ready by then, or whose initialization failed, is
// logged, and each flag keeps its default until it is ready. Only a provider the SDK
// refuses is an error. ctx is the provider's for its lifetime: the process's.
func (e *Evaluator) Use(ctx context.Context, p openfeature.FeatureProvider, instance string) error {
	attrs := map[string]any{}
	if instance != "" {
		attrs[instanceKey] = instance
	}
	e.api.SetEvaluationContext(openfeature.NewTargetlessEvaluationContext(attrs))
	// SetProviderAndWait would cancel the initialization with a deadline: wait apart.
	if err := e.api.SetProvider(ctx, p); err != nil {
		return fmt.Errorf("flags provider: %w", err)
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	timeout := time.NewTimer(e.readyWait)
	defer timeout.Stop()
	for {
		switch state := e.client.State(); state {
		case openfeature.ReadyState:
			return nil
		case openfeature.ErrorState, openfeature.FatalState:
			e.log.Error("flags provider failed to start: each flag has its default", "provider", p.Metadata().Name, "state", string(state))
			return nil
		}
		select {
		case <-tick.C:
		case <-timeout.C:
			e.log.Warn("flags provider not ready yet: each flag has its default meanwhile", "provider", p.Metadata().Name)
			return nil
		case <-ctx.Done():
			return nil
		}
	}
}

// Enabled is the flag's value for an account, by its ID, the only thing of it the
// provider is given; accountID empty: the instance's.
func (e *Evaluator) Enabled(ctx context.Context, name flags.Name, accountID string) bool {
	f, ok := flags.Of(name)
	if !ok {
		e.log.Error("flag not declared", "flag", string(name))
		return false
	}
	ec := openfeature.NewTargetlessEvaluationContext(nil)
	if accountID != "" {
		ec = openfeature.NewEvaluationContext(accountID, nil)
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	d, err := e.client.BooleanValueDetails(ctx, string(name), f.Default, ec)
	switch {
	case err == nil:
		e.failing.Delete(name)
		return d.Value
	case d.ErrorCode == openfeature.FlagNotFoundCode || d.ErrorCode == openfeature.ProviderNotReadyCode:
		// Not set, or not yet known: the default, as shipped.
		e.failing.Delete(name)
	default:
		if prev, _ := e.failing.Swap(name, d.ErrorCode); prev != d.ErrorCode {
			e.log.Warn("flag not evaluated: its default until the provider answers", "flag", string(name),
				"default", f.Default, "error", err)
		}
	}
	return f.Default
}

// Shutdown stops the provider.
func (e *Evaluator) Shutdown(ctx context.Context) error {
	if err := e.api.Shutdown(ctx); err != nil {
		return fmt.Errorf("flags provider: %w", err)
	}
	return nil
}

// Env is the core's provider: the values RUNSTEN_FLAGS gives (flags.Parse), the same for
// every account of the instance; a flag it does not give has its default. No server, no
// file: what a Raspberry Pi runs.
func Env(values map[flags.Name]bool) openfeature.FeatureProvider {
	set := make(map[string]memprovider.InMemoryFlag, len(values))
	for name, on := range values {
		set[string(name)] = memprovider.InMemoryFlag{
			Key: string(name), State: memprovider.Enabled, DefaultVariant: "set",
			Variants: map[string]any{"set": on},
		}
	}
	return memprovider.NewInMemoryProvider(set)
}
