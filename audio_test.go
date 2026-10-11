package main

import (
	"math"
	"path/filepath"
	"testing"
)

func TestVariant(t *testing.T) {
	rate, in := 1000, make([]float64, 2000) // two seconds of a 50 Hz tone, at half volume
	for i := range in {
		in[i] = 0.5 * math.Sin(2*math.Pi*50*float64(i)/float64(rate))
	}
	f := filepath.Join(t.TempDir(), "tone.wav")
	if err := writeWav(f, in, rate); err != nil {
		t.Fatal(err)
	}
	back, r, err := readWav(f)
	if err != nil || r != rate || len(back) != len(in) || math.Abs(back[100]-in[100]) > 1e-4 {
		t.Fatalf("round trip: rate %d, %d samples, err %v", r, len(back), err)
	}
	out := variant(back, rate, 1.25, 0.5)
	if len(out) != 1600 {
		t.Errorf("1.25 times as fast: want 1600 samples, got %d", len(out))
	}
	if out[0] != 0 || math.Abs(out[len(out)-1]) > 0.01 { // faded in from silence, out to it
		t.Errorf("fades: starts at %g, ends at %g", out[0], out[len(out)-1])
	}
	peak := 0.0
	for _, s := range out[600:1000] { // the middle: no fade, and the tone's 62.5 Hz now
		peak = max(peak, math.Abs(s))
	}
	if math.Abs(peak-0.5) > 0.02 {
		t.Errorf("middle should be untouched at 0.5, peaks at %g", peak)
	}
}
