package webrtcvad

import (
	"context"
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	_ "embed"
)

//go:embed wasm-bridge/build/webrtcvad_bridge.wasm
var vadWasmBinary []byte

type wasmFunctions struct {
	malloc   api.Function
	free     api.Function
	create   api.Function
	destroy  api.Function
	init     api.Function
	setMode  api.Function
	process  api.Function
	valid    api.Function
	debugNum api.Function
	debugDen api.Function
}

type wasmContext struct {
	runtime   wazero.Runtime
	module    api.Module
	memory    api.Memory
	functions wasmFunctions
}

var (
	globalWasmContext *wasmContext
	wasmInitOnce      sync.Once
	wasmInitErr       error
)

func getWasmContext(ctx context.Context) (*wasmContext, error) {
	wasmInitOnce.Do(func() {
		rt := wazero.NewRuntime(ctx)
		wasi_snapshot_preview1.MustInstantiate(ctx, rt)

		compiled, err := rt.CompileModule(ctx, vadWasmBinary)
		if err != nil {
			wasmInitErr = fmt.Errorf("compile wasm: %w", err)
			rt.Close(ctx)
			return
		}

		mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("webrtcvad"))
		if err != nil {
			wasmInitErr = fmt.Errorf("instantiate wasm: %w", err)
			compiled.Close(ctx)
			rt.Close(ctx)
			return
		}

		memory := mod.ExportedMemory("memory")
		if memory == nil {
			wasmInitErr = fmt.Errorf("wasm memory export not found")
			mod.Close(ctx)
			compiled.Close(ctx)
			rt.Close(ctx)
			return
		}

		load := func(name string) api.Function {
			fn := mod.ExportedFunction(name)
			if fn == nil && wasmInitErr == nil {
				wasmInitErr = fmt.Errorf("wasm function %s not found", name)
			}
			return fn
		}

		functions := wasmFunctions{
			malloc:   load("malloc"),
			free:     load("free"),
			create:   load("bridge_vad_create"),
			destroy:  load("bridge_vad_free"),
			init:     load("bridge_vad_init"),
			setMode:  load("bridge_vad_set_mode"),
			process:  load("bridge_vad_process"),
			valid:    load("bridge_vad_valid_rate_and_frame_length"),
			debugNum: load("bridge_debug_last_div_num"),
			debugDen: load("bridge_debug_last_div_den"),
		}

		compiled.Close(ctx)

		if wasmInitErr != nil {
			mod.Close(ctx)
			rt.Close(ctx)
			return
		}

		globalWasmContext = &wasmContext{
			runtime:   rt,
			module:    mod,
			memory:    memory,
			functions: functions,
		}
	})

	if wasmInitErr != nil {
		return nil, wasmInitErr
	}

	return globalWasmContext, nil
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

func (wc *wasmContext) lastDivArgs(ctx context.Context) (int32, int32, error) {
	if wc.functions.debugNum == nil || wc.functions.debugDen == nil {
		return 0, 0, fmt.Errorf("debug functions unavailable")
	}
	num, err := wc.callInt32(ctx, wc.functions.debugNum)
	if err != nil {
		return 0, 0, err
	}
	den, err := wc.callInt32(ctx, wc.functions.debugDen)
	if err != nil {
		return 0, 0, err
	}
	return num, den, nil
}
