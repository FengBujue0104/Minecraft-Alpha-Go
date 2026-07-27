// Package audio provides tiny procedural sounds without external asset files.
package audio

import (
	"encoding/binary"
	"math"
	"runtime"

	rl "github.com/gen2brain/raylib-go/raylib"
)

const sampleRate = 22050

// Effects owns the short block place and break sounds.
type Effects struct {
	place  rl.Sound
	remove rl.Sound
	ready  bool
}

// New initializes raylib audio and creates two short synthesized effects.
func New() *Effects {
	rl.InitAudioDevice()
	if !rl.IsAudioDeviceReady() {
		return &Effects{}
	}
	e := &Effects{
		place:  makeTone(580, 0.07),
		remove: makeTone(180, 0.10),
		ready:  true,
	}
	rl.SetSoundVolume(e.place, 0.35)
	rl.SetSoundVolume(e.remove, 0.40)
	return e
}

func makeTone(frequency, duration float64) rl.Sound {
	frames := int(float64(sampleRate) * duration)
	data := make([]byte, frames*2)
	for i := 0; i < frames; i++ {
		t := float64(i) / sampleRate
		// A quick fade in/out avoids clicks at the sample boundaries.
		envelope := math.Min(1, math.Min(t/0.008, (duration-t)/0.025))
		sample := int16(math.Sin(2*math.Pi*frequency*t) * envelope * 0.32 * 32767)
		binary.LittleEndian.PutUint16(data[i*2:], uint16(sample))
	}
	wave := rl.NewWave(uint32(frames), sampleRate, 16, 1, data)
	sound := rl.LoadSoundFromWave(wave)
	// NewWave points directly at Go-owned memory. UnloadWave is only valid for
	// waves allocated by raylib; calling it here makes raylib free Go memory and
	// crashes immediately after audio device startup. LoadSoundFromWave copies
	// the samples into its own audio buffer, so retaining data through this call
	// is sufficient and there is no Wave resource to unload.
	runtime.KeepAlive(data)
	return sound
}

// PlayPlace plays the placement sound when audio is available.
func (e *Effects) PlayPlace() {
	if e.ready {
		rl.PlaySound(e.place)
	}
}

// PlayBreak plays the removal sound when audio is available.
func (e *Effects) PlayBreak() {
	if e.ready {
		rl.PlaySound(e.remove)
	}
}

// Close releases the audio resources created by New.
func (e *Effects) Close() {
	if !e.ready {
		return
	}
	rl.UnloadSound(e.place)
	rl.UnloadSound(e.remove)
	rl.CloseAudioDevice()
	e.ready = false
}
