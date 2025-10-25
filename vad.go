package webrtcvad

import (
	"context"
	"errors"
	"fmt"
)

// VadInst represents a VAD instance stored inside the wasm module.
type VadInst struct {
	ptr uint32
}

// Create creates an instance of the WebRTC VAD.
// If the underlying wasm runtime fails to initialise, an empty instance is returned.
func Create() VadInst {
	ctx := context.Background()
	wc, err := getWasmContext(ctx)
	if err != nil {
		return VadInst{}
	}

	ptr, err := wc.callUint32(ctx, wc.functions.create)
	if err != nil || ptr == 0 {
		return VadInst{}
	}

	return VadInst{ptr: ptr}
}

// Free releases the dynamic memory of a specified VAD instance.
func Free(v VadInst) {
	if v.ptr == 0 {
		return
	}

	ctx := context.Background()
	wc, err := getWasmContext(ctx)
	if err != nil {
		return
	}

	_ = wc.callVoid(ctx, wc.functions.destroy, uint64(v.ptr))
}

// Init initialises a VAD instance.
func Init(v VadInst) error {
	if v.ptr == 0 {
		return errors.New("vad instance is uninitialised")
	}

	ctx := context.Background()
	wc, err := getWasmContext(ctx)
	if err != nil {
		return err
	}

	result, err := wc.callInt32(ctx, wc.functions.init, uint64(v.ptr))
	if err != nil {
		return fmt.Errorf("wasm init call failed: %w", err)
	}
	if result == -1 {
		return errors.New("null pointer or default mode could not be set")
	}
	return nil
}

// SetMode sets the VAD operating mode.
func SetMode(v VadInst, mode int) error {
	if v.ptr == 0 {
		return errors.New("vad instance is uninitialised")
	}

	ctx := context.Background()
	wc, err := getWasmContext(ctx)
	if err != nil {
		return err
	}

	result, err := wc.callInt32(ctx, wc.functions.setMode, uint64(v.ptr), uint64(uint32(mode)))
	if err != nil {
		return fmt.Errorf("wasm set_mode call failed: %w", err)
	}
	if result == -1 {
		return errors.New("mode could not be set or the VAD instance has not been initialized")
	}
	return nil
}

// Process returns whether the given frame is classified as active speech.
func Process(v VadInst, fs int, audioFrame []byte, frameLength int) (bool, error) {
	if v.ptr == 0 {
		return false, errors.New("vad instance is uninitialised")
	}

	if frameLength <= 0 {
		return false, fmt.Errorf("invalid frame length: %d", frameLength)
	}

	expectedBytes := frameLength * 2
	if expectedBytes < 0 {
		return false, errors.New("frame length overflow")
	}
	if len(audioFrame) < expectedBytes {
		return false, fmt.Errorf("audio frame too short: have %d bytes, need %d bytes", len(audioFrame), expectedBytes)
	}

	ctx := context.Background()
	wc, err := getWasmContext(ctx)
	if err != nil {
		return false, err
	}

	framePtr, err := wc.writeToMemory(ctx, audioFrame[:expectedBytes])
	if err != nil {
		return false, err
	}
	defer wc.freeMemory(ctx, framePtr)

	result, err := wc.callInt32(
		ctx,
		wc.functions.process,
		uint64(v.ptr),
		uint64(uint32(fs)),
		uint64(framePtr),
		uint64(uint32(frameLength)),
	)
	if err != nil {
		return false, fmt.Errorf("wasm process call failed: %w", err)
	}

	switch result {
	case 1:
		return true, nil
	case 0:
		return false, nil
	default:
		return false, errors.New("process fail")
	}
}

// ValidRateAndFrameLength verifies the input rate and frame length.
func ValidRateAndFrameLength(rate int, frameLength int) bool {
	if frameLength < 0 {
		return false
	}

	ctx := context.Background()
	wc, err := getWasmContext(ctx)
	if err != nil {
		return false
	}

	result, err := wc.callInt32(
		ctx,
		wc.functions.valid,
		uint64(uint32(rate)),
		uint64(uint32(frameLength)),
	)
	if err != nil {
		return false
	}

	return result == 0
}
