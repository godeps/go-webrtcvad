package webrtcvad

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	_ "embed"
)

//go:embed wasm-bridge/build/webrtcvad_bridge.wasm
var vadWasmBinary []byte

type wasmFunctions struct {
	malloc  api.Function
	free    api.Function
	create  api.Function
	destroy api.Function
	init    api.Function
	setMode api.Function
	process api.Function
	valid   api.Function
}

type wasmContext struct {
	runtime   wazero.Runtime
	module    api.Module
	memory    api.Memory
	functions wasmFunctions
	mu        sync.Mutex
}

type wasmContextPool struct {
	ch chan *wasmContext
}

var (
	globalWasmContext *wasmContext
	wasmInitOnce      sync.Once
	wasmInitErr       error
	wasmRuntime       wazero.Runtime
	wasmCompiled      wazero.CompiledModule
	wasmModuleConfig  wazero.ModuleConfig
	wasmPool          *wasmContextPool
	wasmCloseOnce     sync.Once
	moduleNameCounter uint64
)

func getWasmContext(ctx context.Context) (*wasmContext, error) {
	wasmInitOnce.Do(func() {
		rt := wazero.NewRuntime(ctx)
		wasi_snapshot_preview1.MustInstantiate(ctx, rt)

		compiled, err := rt.CompileModule(ctx, vadWasmBinary)
		if err != nil {
			wasmInitErr = fmt.Errorf("compile wasm: %w", err)
			_ = rt.Close(ctx)
			return
		}

		wasmRuntime = rt
		wasmCompiled = compiled
		wasmModuleConfig = wazero.NewModuleConfig()

		poolSize := runtime.NumCPU()
		if poolSize < 2 {
			poolSize = 2
		}
		wasmPool = newWasmContextPool(poolSize)

		globalWasmContext, wasmInitErr = newWasmContext(ctx)
		if wasmInitErr != nil {
			wasmPool = nil
			_ = compiled.Close(ctx)
			_ = rt.Close(ctx)
			return
		}

		wasmPool.put(globalWasmContext)
	})

	if wasmInitErr != nil {
		return nil, wasmInitErr
	}

	return wasmPool.get(ctx)
}

func releaseWasmContext(wc *wasmContext) {
	if wasmPool == nil {
		if wc != nil {
			wc.close(context.Background())
		}
		return
	}
	wasmPool.put(wc)
}

func newWasmContextPool(size int) *wasmContextPool {
	return &wasmContextPool{ch: make(chan *wasmContext, size)}
}

func (p *wasmContextPool) get(ctx context.Context) (*wasmContext, error) {
	if p == nil {
		return nil, fmt.Errorf("wasm context pool is uninitialised")
	}
	select {
	case wc := <-p.ch:
		if wc == nil {
			return nil, fmt.Errorf("retrieved nil wasm context from pool")
		}
		return wc, nil
	default:
	}
	return newWasmContext(ctx)
}

func (p *wasmContextPool) put(wc *wasmContext) {
	if p == nil || wc == nil {
		return
	}
	select {
	case p.ch <- wc:
	default:
		wc.close(context.Background())
	}
}

func (p *wasmContextPool) close(ctx context.Context) {
	if p == nil {
		return
	}
	for {
		select {
		case wc := <-p.ch:
			if wc != nil {
				wc.close(ctx)
			}
		default:
			return
		}
	}
}

func newWasmContext(ctx context.Context) (*wasmContext, error) {
	if wasmRuntime == nil || wasmCompiled == nil || wasmModuleConfig == nil {
		return nil, fmt.Errorf("wasm runtime is uninitialised")
	}

	name := fmt.Sprintf("webrtcvad-%d", atomic.AddUint64(&moduleNameCounter, 1))
	cfg := wasmModuleConfig.WithName(name)

	mod, err := wasmRuntime.InstantiateModule(ctx, wasmCompiled, cfg)
	if err != nil {
		return nil, fmt.Errorf("instantiate wasm: %w", err)
	}

	memory := mod.ExportedMemory("memory")
	if memory == nil {
		mod.Close(ctx)
		return nil, fmt.Errorf("wasm memory export not found")
	}

	load := func(fnName string) (api.Function, error) {
		fn := mod.ExportedFunction(fnName)
		if fn == nil {
			return nil, fmt.Errorf("wasm function %s not found", fnName)
		}
		return fn, nil
	}

	mallocFn, err := load("malloc")
	if err != nil {
		mod.Close(ctx)
		return nil, err
	}
	freeFn, err := load("free")
	if err != nil {
		mod.Close(ctx)
		return nil, err
	}
	createFn, err := load("bridge_vad_create")
	if err != nil {
		mod.Close(ctx)
		return nil, err
	}
	destroyFn, err := load("bridge_vad_free")
	if err != nil {
		mod.Close(ctx)
		return nil, err
	}
	initFn, err := load("bridge_vad_init")
	if err != nil {
		mod.Close(ctx)
		return nil, err
	}
	setModeFn, err := load("bridge_vad_set_mode")
	if err != nil {
		mod.Close(ctx)
		return nil, err
	}
	processFn, err := load("bridge_vad_process")
	if err != nil {
		mod.Close(ctx)
		return nil, err
	}
	validFn, err := load("bridge_vad_valid_rate_and_frame_length")
	if err != nil {
		mod.Close(ctx)
		return nil, err
	}

	return &wasmContext{
		runtime: wasmRuntime,
		module:  mod,
		memory:  memory,
		functions: wasmFunctions{
			malloc:  mallocFn,
			free:    freeFn,
			create:  createFn,
			destroy: destroyFn,
			init:    initFn,
			setMode: setModeFn,
			process: processFn,
			valid:   validFn,
		},
	}, nil
}

func (wc *wasmContext) close(ctx context.Context) {
	if wc == nil || wc.module == nil {
		return
	}
	_ = wc.module.Close(ctx)
	wc.module = nil
	wc.memory = nil
	wc.functions = wasmFunctions{}
}

func (wc *wasmContext) callUint32(ctx context.Context, fn api.Function, args ...uint64) (uint32, error) {
	if fn == nil {
		return 0, fmt.Errorf("wasm function is nil")
	}
	results, err := fn.Call(ctx, args...)
	if err != nil {
		return 0, err
	}
	if len(results) == 0 {
		return 0, fmt.Errorf("wasm function returned no results")
	}
	return uint32(results[0]), nil
}

func (wc *wasmContext) callInt32(ctx context.Context, fn api.Function, args ...uint64) (int32, error) {
	if fn == nil {
		return 0, fmt.Errorf("wasm function is nil")
	}
	results, err := fn.Call(ctx, args...)
	if err != nil {
		return 0, err
	}
	if len(results) == 0 {
		return 0, fmt.Errorf("wasm function returned no results")
	}
	return int32(results[0]), nil
}

func (wc *wasmContext) callVoid(ctx context.Context, fn api.Function, args ...uint64) error {
	if fn == nil {
		return fmt.Errorf("wasm function is nil")
	}
	_, err := fn.Call(ctx, args...)
	return err
}

func (wc *wasmContext) mallocBytes(ctx context.Context, byteCount uint32) (uint32, error) {
	if byteCount == 0 {
		return 0, nil
	}
	ptr, err := wc.callUint32(ctx, wc.functions.malloc, uint64(byteCount))
	if err != nil {
		return 0, fmt.Errorf("wasm malloc failed: %w", err)
	}
	if ptr == 0 {
		return 0, fmt.Errorf("wasm malloc returned zero pointer for %d bytes", byteCount)
	}
	return ptr, nil
}

func (wc *wasmContext) freeMemory(ctx context.Context, ptr uint32) error {
	if ptr == 0 {
		return nil
	}
	if wc.functions.free == nil {
		return fmt.Errorf("wasm free function is nil")
	}
	_, err := wc.functions.free.Call(ctx, uint64(ptr))
	if err != nil {
		return fmt.Errorf("wasm free failed: %w", err)
	}
	return nil
}

func (wc *wasmContext) writeToMemory(ctx context.Context, data []byte) (uint32, error) {
	if len(data) == 0 {
		return 0, nil
	}
	ptr, err := wc.mallocBytes(ctx, uint32(len(data)))
	if err != nil {
		return 0, err
	}
	if !wc.memory.Write(ptr, data) {
		_ = wc.freeMemory(ctx, ptr)
		return 0, fmt.Errorf("failed to write %d bytes to wasm memory at %d", len(data), ptr)
	}
	return ptr, nil
}

// Shutdown releases all pooled contexts and closes the underlying wasm runtime.
// It should be called when the application no longer needs the VAD.
func Shutdown(ctx context.Context) error {
	var shutdownErr error
	wasmCloseOnce.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}

		if wasmPool != nil {
			wasmPool.close(ctx)
			wasmPool = nil
		}

		globalWasmContext = nil

		if wasmCompiled != nil {
			if err := wasmCompiled.Close(ctx); err != nil {
				shutdownErr = err
			}
			wasmCompiled = nil
		}

		if wasmRuntime != nil {
			if err := wasmRuntime.Close(ctx); err != nil && shutdownErr == nil {
				shutdownErr = err
			}
			wasmRuntime = nil
		}

		wasmModuleConfig = nil
		if shutdownErr == nil {
			wasmInitErr = fmt.Errorf("wasm runtime shut down")
		}
	})
	return shutdownErr
}
