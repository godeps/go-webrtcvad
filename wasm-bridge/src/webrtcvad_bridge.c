#include <stdint.h>
#include <stddef.h>
#include "export.h"
#include "common_audio/vad/include/webrtc_vad.h"

extern int32_t webrtcvad_last_div_num;
extern int16_t webrtcvad_last_div_den;

static inline VadInst* ptr_to_vad(uint32_t ptr) {
	return (VadInst*)(uintptr_t)ptr;
}

static inline const int16_t* ptr_to_int16(uint32_t ptr) {
	return (const int16_t*)(uintptr_t)ptr;
}

EXPORT(bridge_vad_create)
uint32_t
bridge_vad_create(void)
{
	return (uint32_t)(uintptr_t)WebRtcVad_Create();
}

EXPORT(bridge_vad_free)
void
bridge_vad_free(uint32_t handle_ptr)
{
	WebRtcVad_Free(ptr_to_vad(handle_ptr));
}

EXPORT(bridge_vad_init)
int32_t
bridge_vad_init(uint32_t handle_ptr)
{
	return WebRtcVad_Init(ptr_to_vad(handle_ptr));
}

EXPORT(bridge_vad_set_mode)
int32_t
bridge_vad_set_mode(uint32_t handle_ptr, int32_t mode)
{
	return WebRtcVad_set_mode(ptr_to_vad(handle_ptr), mode);
}

EXPORT(bridge_vad_process)
int32_t
bridge_vad_process(uint32_t handle_ptr, int32_t fs, uint32_t audio_frame_ptr, uint32_t frame_length)
{
	return WebRtcVad_Process(ptr_to_vad(handle_ptr), fs, ptr_to_int16(audio_frame_ptr), (size_t)frame_length);
}

EXPORT(bridge_vad_valid_rate_and_frame_length)
int32_t
bridge_vad_valid_rate_and_frame_length(int32_t rate, uint32_t frame_length)
{
	return WebRtcVad_ValidRateAndFrameLength(rate, (size_t)frame_length);
}

EXPORT(bridge_debug_last_div_num)
int32_t
bridge_debug_last_div_num(void)
{
	return webrtcvad_last_div_num;
}

EXPORT(bridge_debug_last_div_den)
int32_t
bridge_debug_last_div_den(void)
{
	return (int32_t)webrtcvad_last_div_den;
}
