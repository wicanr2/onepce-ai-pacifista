#!/usr/bin/env python3
"""比對兩個 WAV 的音高軌跡與 RMS 包絡（docs/spec/psg.md §6.3）。

用法：compare_wav.py <ours.wav> <oracle.wav> [--window 1024] [--min-agree 0.95] [--min-rms-corr 0.9]

兩邊先各自對齊到第一個非靜音樣本，再以固定視窗切段：每段取 FFT 主頻率（只在兩邊都有能量的
段落比，相差 ≤ 1 bin 算一致），另取每段 RMS 做 Pearson 相關。不要求逐 sample 相同——
oracle（Mednafen）有自己的重取樣與低通，本版是零階保持。
需要 numpy；在 nectaris-frame-parity 之類的分析容器裡跑。
"""
import argparse
import sys
import wave

import numpy as np


def load_mono(path):
    with wave.open(path, "rb") as w:
        rate = w.getframerate()
        n = w.getnframes()
        ch = w.getnchannels()
        width = w.getsampwidth()
        raw = w.readframes(n)
    if width != 2:
        raise SystemExit(f"{path}: {width * 8}-bit not supported")
    data = np.frombuffer(raw, dtype="<i2").astype(np.float64) / 32768.0
    if ch > 1:
        data = data.reshape(-1, ch).mean(axis=1)
    return rate, data


def onset(x, threshold=0.01):
    idx = np.flatnonzero(np.abs(x) > threshold)
    return int(idx[0]) if len(idx) else 0


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("ours")
    ap.add_argument("oracle")
    ap.add_argument("--window", type=int, default=1024)
    ap.add_argument("--min-agree", type=float, default=0.95)
    ap.add_argument("--min-rms-corr", type=float, default=0.9)
    args = ap.parse_args()

    ra, a = load_mono(args.ours)
    rb, b = load_mono(args.oracle)
    if ra != rb:
        raise SystemExit(f"sample rates differ: {ra} vs {rb}")
    a = a[onset(a):]
    b = b[onset(b):]
    n = min(len(a), len(b)) // args.window * args.window
    a, b = a[:n], b[:n]
    wa = a.reshape(-1, args.window)
    wb = b.reshape(-1, args.window)
    rms_a = np.sqrt((wa ** 2).mean(axis=1))
    rms_b = np.sqrt((wb ** 2).mean(axis=1))
    floor = 0.02 * max(rms_a.max(), rms_b.max())
    active = (rms_a > floor) & (rms_b > floor)
    hann = np.hanning(args.window)
    fa = np.abs(np.fft.rfft(wa * hann, axis=1))
    fb = np.abs(np.fft.rfft(wb * hann, axis=1))
    fa[:, 0] = 0
    fb[:, 0] = 0
    peak_a = fa.argmax(axis=1)
    peak_b = fb.argmax(axis=1)
    agree = np.abs(peak_a[active] - peak_b[active]) <= 1
    agree_rate = agree.mean() if active.any() else 0.0
    rms_corr = float(np.corrcoef(rms_a, rms_b)[0, 1]) if len(rms_a) > 2 else 0.0
    bin_hz = ra / args.window
    print(f"windows={len(wa)} active={int(active.sum())} bin={bin_hz:.1f}Hz")
    print(f"pitch_agreement={agree_rate:.4f} (min {args.min_agree})")
    print(f"rms_envelope_corr={rms_corr:.4f} (min {args.min_rms_corr})")
    print(f"rms_ratio_ours_over_oracle={rms_a.mean() / max(rms_b.mean(), 1e-9):.3f}")
    ok = agree_rate >= args.min_agree and rms_corr >= args.min_rms_corr
    print("RESULT", "PASS" if ok else "FAIL")
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
