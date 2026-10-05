#!/usr/bin/env python3
"""Cleans meow recordings: writes a cleaned copy of each .wav given into ~/.cchobi/meows/
(where the pet plays them from).

  python3 tools/clean_meows.py recordings/*.wav

Two passes:
  1. spectral gating: learn the steady background noise from the clip's quietest moments and turn
     every frequency down wherever it isn't clearly louder than that noise
  2. harmonic masking: a meow is a pitched sound, a fundamental plus overtones at exact multiples
     of it. Track her pitch, keep only those overtones, and silence everything between them, and
     any moment without a meow-like pitch (that's also what removes people talking: voices sit
     lower, around 100-250 Hz).
"""
import sys, wave
from pathlib import Path
import numpy as np

N, HOP = 2048, 256
CAT_PITCH = (300, 1400)   # Hz: her fundamental lives in here; human voices sit below
OUT = Path.home() / '.cchobi' / 'meows'
VOLUME_DB = -13           # peak level of the result
BREATH = 0.25             # how much of the sound between overtones survives inside a meow (-12 dB)
TAIL = 0.25               # seconds a meow is let to trail off after the detector loses it


def stft(s):
    win = np.hanning(N)
    pad = np.pad(s, (N, N))
    return np.fft.rfft(np.array([pad[i:i + N] * win for i in range(0, len(pad) - N, HOP)]), axis=1)


def istft(X, length):
    win = np.hanning(N)
    frames = np.fft.irfft(X, N, axis=1) * win
    out, norm = np.zeros(length + 2 * N), np.zeros(length + 2 * N)
    for i, fr in enumerate(frames):
        out[i * HOP:i * HOP + N] += fr
        norm[i * HOP:i * HOP + N] += win ** 2
    return (out / np.maximum(norm, 1e-6))[N:N + length]


def smooth(a, k, axis):
    return np.apply_along_axis(lambda v: np.convolve(v, np.ones(k) / k, 'same'), axis, a)


def gate(X, freqs):
    """Pass 1: subtract the steady background noise."""
    mag = np.abs(X)
    quiet = mag.sum(1) <= np.percentile(mag.sum(1), 25)
    noise = mag[quiet].mean(0)
    gain = np.clip(1 - 2.5 * noise / (mag + 1e-9), 0.02, 1)
    gain = smooth(smooth(gain, 3, 0), 3, 1)                 # smoother gain = fewer warbly artefacts
    return X * gain * np.clip((freqs - 250) / 100, 0, 1)    # and nothing below ~300 Hz at all


def best_pitch(a, freqs, cands):
    """The candidate pitch whose overtones line up best with the (compressed) spectrum a."""
    harm = np.arange(1, 13)
    best, best_score = cands[0], -1e18
    for f0 in cands:
        on = harm * f0
        off = (harm + 0.5) * f0                             # between overtones: should be empty
        on, off = on[on < freqs[-1]], off[off < freqs[-1]]
        w_on, w_off = 1 / np.sqrt(np.arange(1, len(on) + 1)), 1 / np.sqrt(np.arange(1, len(off) + 1))
        score = (np.interp(on, freqs, a) * w_on).sum() - 0.8 * (np.interp(off, freqs, a) * w_off).sum()
        if score > best_score:
            best, best_score = f0, score
    return best


def explained(m, f0, freqs):
    """How much of a frame's energy (above 300 Hz) sits on the overtones of f0, 0..1."""
    hi = freqs > 300
    e = (m[hi] ** 2).sum()
    return (m[hi] ** 2 * harmonic_mask(f0, freqs, freqs[1])[hi]).sum() / e if e else 0


def pitch_track(mag, freqs):
    """Her pitch in each frame (Hz) and how much of the frame's energy it explains (0..1)."""
    cands = np.arange(*CAT_PITCH, 3.0)
    f0s = np.array([best_pitch(np.sqrt(m), freqs, cands) for m in mag])  # sqrt: one loud overtone can't win alone
    conf = np.array([explained(m, f, freqs) for m, f in zip(mag, f0s)])
    return f0s, conf


def harmonic_mask(f0, freqs, bin_hz):
    """1 on each overtone of f0 (a little wider higher up, where pitch wobble spreads them), 0 between."""
    mask = np.zeros(len(freqs))
    for h in range(1, int(freqs[-1] / f0) + 1):
        width = 2.5 * bin_hz + 0.02 * h * f0
        mask = np.maximum(mask, np.exp(-0.5 * ((freqs - h * f0) / width) ** 2))
    return mask


def meow_runs(f0s, conf, energy, min_frames, mag, freqs):
    """Which frames are her meowing: long stretches whose pitch glides smoothly. Leftover noise
    can look pitched for a moment, but only a real meow keeps a steady pitch for a while.
    Updates f0s in place where it follows a meow out into its quiet start and end."""
    voiced = (conf > 0.5) & (energy > energy.max() - 30)
    keep = np.zeros(len(f0s), bool)
    start = None
    for t in range(len(f0s) + 1):
        smooth_on = (t < len(f0s) and voiced[t] and
                     (start is None or abs(f0s[t] / f0s[t - 1] - 1) < 0.08))
        if smooth_on and start is None:
            start = t
        elif not smooth_on and start is not None:
            if t - start >= min_frames:
                keep[start:t] = True
            start = t if t < len(f0s) and voiced[t] else None
    # Follow each meow out into its soft start and fading end. Those frames are too quiet for a
    # pitch guess from scratch, so look only close to the neighbouring frame's pitch.
    for step in (1, -1):
        order = range(1, len(f0s)) if step == 1 else range(len(f0s) - 2, -1, -1)
        for t in order:
            prev = t - step
            if not keep[prev] or keep[t] or energy[t] < energy.max() - 50:
                continue
            near = np.linspace(f0s[prev] * 0.92, f0s[prev] * 1.08, 17)
            f0 = best_pitch(np.sqrt(mag[t]), freqs, near)
            if explained(mag[t], f0, freqs) > 0.3:
                f0s[t], keep[t] = f0, True
    return np.convolve(keep, np.ones(9), 'same') > 0                  # bridge tiny gaps, soften the edges


def presence(meowing, head, tail):
    """0..1 per frame: 1 while meowing, fading in over `head` frames before and out over `tail` after,
    so a meow trails off naturally instead of stopping where the detector lost it."""
    idx = np.flatnonzero(meowing)
    w = meowing.astype(float)
    for t in range(len(w)):
        if not meowing[t] and len(idx):
            before, after = idx[idx < t], idx[idx > t]
            if len(before):
                w[t] = max(w[t], 1 - (t - before[-1]) / tail)
            if len(after):
                w[t] = max(w[t], 1 - (after[0] - t) / head)
    return np.clip(w, 0, 1)


def clean(s, r):
    X = stft(s)
    freqs = np.fft.rfftfreq(N, 1 / r)
    X = gate(X, freqs)
    mag = np.abs(X)
    f0s, conf = pitch_track(mag, freqs)
    energy = 10 * np.log10((mag ** 2).sum(1) + 1e-12)
    meowing = meow_runs(f0s, conf, energy, int(0.12 * r / HOP), mag, freqs)  # at least 0.12 s long
    f0s = np.array([np.median(f0s[max(0, t - 2):t + 3]) for t in range(len(f0s))])  # steadier pitch
    idx = np.flatnonzero(meowing)
    if len(idx):  # in the fade-in/out, keep the pitch the meow had at its start/end
        f0s[:idx[0]], f0s[idx[-1] + 1:] = f0s[idx[0]], f0s[idx[-1]]
    w = presence(meowing, head=int(0.04 * r / HOP), tail=int(TAIL * r / HOP))
    # A tail stops at the first sudden jump in loudness: that's a new sound (a bang, a voice),
    # not her meow fading out.
    for t in range(1, len(w)):
        if not meowing[t] and w[t] > 0 and not meowing[t - 1] and energy[t] > energy[t - 1] + 6:
            end = t
            while end < len(w) and not meowing[end]:
                w[end] = 0
                end += 1
    # Inside a meow, keep its overtones fully but let some of what's between them through
    # (BREATH): that's the breathy texture that stops it sounding robotic. In the fade-out only
    # the overtones carry on, so nothing else sneaks in behind her.
    core = smooth(meowing.astype(float), 5, 0)
    gains = np.array([wt * (c * BREATH + (1 - c * BREATH) * harmonic_mask(f, freqs, freqs[1])) + (1 - wt) * 0.003
                      for f, wt, c in zip(f0s, w, core)])
    gains = smooth(gains, 3, 0)                                       # no clicks where it switches on/off
    y = istft(X * gains, len(s))
    on = np.flatnonzero(w > 0.01)                                     # trim to the meow and its tail
    a, b = (max(0, on[0] * HOP - N), min(len(y), on[-1] * HOP)) if len(on) else (0, len(y))
    return y[a:b], meowing.mean()


def main():
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    OUT.mkdir(parents=True, exist_ok=True)
    for path in sys.argv[1:]:
        with wave.open(path) as w:
            r, ch = w.getframerate(), w.getnchannels()
            s = np.frombuffer(w.readframes(w.getnframes()), np.int16).astype(float).reshape(-1, ch).mean(1)
        y, meowing = clean(s, r)
        fi, fo = int(.01 * r), int(.04 * r)
        y[:fi] *= np.linspace(0, 1, fi)
        y[-fo:] *= np.linspace(1, 0, fo)
        y = y / np.abs(y).max() * 10 ** (VOLUME_DB / 20) * 32767
        out = OUT / Path(path).name
        with wave.open(str(out), 'wb') as o:
            o.setnchannels(1), o.setsampwidth(2), o.setframerate(r), o.writeframes(y.astype(np.int16).tobytes())
        print(f'{out}: {len(y) / r:.2f}s, meowing for {meowing:.0%} of it')


if __name__ == '__main__':
    main()
