package main

// .wav files, and making a sound's variants (faster and higher, slower and lower, with fades)
// without ffmpeg.

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
)

// readWav: a PCM (8/16/24/32-bit) or float .wav, mixed down to mono, -1..1.
func readWav(f string) (samples []float64, rate int, err error) {
	b, err := os.ReadFile(f)
	if err != nil {
		return nil, 0, err
	}
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, 0, errors.New("not a .wav")
	}
	var format, channels, bits int
	for p := 12; p+8 <= len(b); {
		id, size := string(b[p:p+4]), int(binary.LittleEndian.Uint32(b[p+4:]))
		body := b[p+8 : min(p+8+size, len(b))]
		switch id {
		case "fmt ":
			if len(body) < 16 {
				return nil, 0, errors.New("bad fmt chunk")
			}
			format, channels = int(binary.LittleEndian.Uint16(body)), int(binary.LittleEndian.Uint16(body[2:]))
			rate, bits = int(binary.LittleEndian.Uint32(body[4:])), int(binary.LittleEndian.Uint16(body[14:]))
			if format == 0xFFFE && len(body) >= 26 { // WAVE_FORMAT_EXTENSIBLE: the real format is in its GUID
				format = int(binary.LittleEndian.Uint16(body[24:]))
			}
		case "data":
			width := bits / 8
			if channels < 1 || width < 1 || !(format == 1 || format == 3 && width == 4) {
				return nil, 0, errors.New("not PCM or float")
			}
			frame := width * channels
			samples = make([]float64, len(body)/frame)
			for i := range samples {
				sum := 0.0
				for c := range channels {
					sum += sample(body[i*frame+c*width:], width, format == 3)
				}
				samples[i] = sum / float64(channels)
			}
			return samples, rate, nil
		}
		p += 8 + size + size%2
	}
	return nil, 0, errors.New("no data")
}

func sample(b []byte, width int, float bool) float64 {
	switch {
	case float:
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(b)))
	case width == 1:
		return (float64(b[0]) - 128) / 128 // 8-bit is unsigned
	case width == 2:
		return float64(int16(binary.LittleEndian.Uint16(b))) / (1 << 15)
	case width == 3:
		return float64(int32(uint32(b[0])<<8|uint32(b[1])<<16|uint32(b[2])<<24)>>8) / (1 << 23)
	default:
		return float64(int32(binary.LittleEndian.Uint32(b))) / (1 << 31)
	}
}

// pcm16: samples as 16-bit little-endian PCM.
func pcm16(samples []float64) []byte {
	out := make([]byte, 2*len(samples))
	for i, s := range samples {
		binary.LittleEndian.PutUint16(out[2*i:], uint16(int16(math.Round(max(-1, min(1, s))*32767))))
	}
	return out
}

func writeWav(f string, samples []float64, rate int) error {
	data := pcm16(samples)
	h := binary.LittleEndian.AppendUint32([]byte("RIFF"), uint32(36+len(data)))
	h = binary.LittleEndian.AppendUint32(append(h, "WAVEfmt "...), 16)
	h = binary.LittleEndian.AppendUint16(h, 1) // PCM
	h = binary.LittleEndian.AppendUint16(h, 1) // mono
	h = binary.LittleEndian.AppendUint32(h, uint32(rate))
	h = binary.LittleEndian.AppendUint32(h, uint32(rate*2))
	h = binary.LittleEndian.AppendUint16(h, 2)
	h = binary.LittleEndian.AppendUint16(h, 16)
	h = append(h, "data"...)
	h = binary.LittleEndian.AppendUint32(h, uint32(len(data)))
	return os.WriteFile(f, append(h, data...), 0o644)
}

// variant: the sound played speed times as fast (so also that much higher), at the same sample
// rate, after fading it in and out over fade seconds each (on an S-curve), if fade > 0.
// ponytail: linear interpolation, so a sped-up sound can alias a little above ~10 kHz; a windowed
// sinc if anyone hears it (ffmpeg's resampler did that before).
func variant(in []float64, rate int, speed, fade float64) []float64 {
	n := int(fade * float64(rate))
	if 2*n > len(in) {
		n = len(in) / 2
	}
	faded := make([]float64, len(in))
	copy(faded, in)
	for i := range n {
		g := (1 - math.Cos(math.Pi*float64(i)/float64(n))) / 2
		faded[i] *= g
		faded[len(faded)-1-i] *= g
	}
	out := make([]float64, int(float64(len(in))/speed))
	for i := range out {
		at := float64(i) * speed
		j, frac := int(at), at-math.Floor(at)
		next := faded[min(j+1, len(faded)-1)]
		out[i] = faded[j]*(1-frac) + next*frac
	}
	return out
}
