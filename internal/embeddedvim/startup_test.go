package embeddedvim

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// Observe the context boundary without sleeps, CPU contention or a warm global
// cache hiding the cold-start path. Only public linked program bytes are used.
type startupRuntime struct {
	wazero.Runtime
	caller       context.Context
	afterCompile func()
	wrongCompile bool
	instantiated bool
	guestBounded bool
}

func (r *startupRuntime) CompileModule(ctx context.Context, wasm []byte) (wazero.CompiledModule, error) {
	if ctx != r.caller {
		r.wrongCompile = true
		return nil, errors.New("synthetic compilation used guest execution budget")
	}
	compiled, err := r.Runtime.CompileModule(ctx, wasm)
	if r.afterCompile != nil {
		r.afterCompile()
	}
	return compiled, err
}

func (r *startupRuntime) InstantiateModule(ctx context.Context, compiled wazero.CompiledModule, config wazero.ModuleConfig) (api.Module, error) {
	r.instantiated = true
	deadline, ok := ctx.Deadline()
	r.guestBounded = ok && time.Until(deadline) > 0 && time.Until(deadline) <= 10*time.Second
	return r.Runtime.InstantiateModule(ctx, compiled, config)
}

func TestColdCompilationDoesNotConsumeGuestStartupBudget(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "without-caller-deadline"
		if deadline {
			name = "with-caller-deadline"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			if deadline {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Minute)
				defer cancel()
			}
			r, s := coldStartupSession(ctx, t)
			a, err := BundledAssets()
			if err != nil {
				t.Fatal("public startup fixture unavailable")
			}
			if s.initialize(ctx, a.Wasm, a.Guest, "synthetic startup", 64<<20) != nil || r.wrongCompile {
				t.Fatal("cold public compilation consumed the guest startup budget")
			}
			if !r.instantiated || !r.guestBounded || s.Snapshot().Text != "synthetic startup" {
				t.Fatal("guest startup lost its execution bound or acknowledgement")
			}
		})
	}
}

func TestCallerCancellationDuringCompilationPreventsGuestStartup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, s := coldStartupSession(ctx, t)
	r.afterCompile = cancel
	a, err := BundledAssets()
	if err != nil {
		t.Fatal("public startup fixture unavailable")
	}
	if err = s.initialize(ctx, a.Wasm, a.Guest, "synthetic startup", 64<<20); !errors.Is(err, ErrGuest) || r.wrongCompile {
		t.Fatal("caller cancellation during compilation was not preserved")
	}
	if r.instantiated || s.module != nil || s.jsctx != 0 || s.Snapshot().Text != "" {
		t.Fatal("cancelled compilation started a guest or acknowledged input")
	}
}

func coldStartupSession(ctx context.Context, t *testing.T) (*startupRuntime, *Session) {
	t.Helper()
	cache := wazero.NewCompilationCache()
	r := &startupRuntime{
		Runtime: wazero.NewRuntimeWithConfig(context.Background(), runtimeConfig().WithCompilationCache(cache)),
		caller:  ctx,
	}
	s := &Session{runtime: r, budget: 500 * time.Millisecond}
	t.Cleanup(func() {
		if s.Close() != nil || cache.Close(context.Background()) != nil {
			t.Error("owned startup runtime cleanup failed")
		}
	})
	return r, s
}
