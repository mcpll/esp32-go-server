#!/usr/bin/env python3
"""Summarize websocket_multi JSONL metrics."""

from __future__ import annotations

import argparse
import json
from collections import Counter
from pathlib import Path
from statistics import mean
from typing import Iterable, List


def percentile(values: List[float], p: float) -> float:
    if not values:
        return 0.0
    if p <= 0:
        return values[0]
    if p >= 100:
        return values[-1]
    idx = (len(values) - 1) * (p / 100.0)
    lo = int(idx)
    hi = min(lo + 1, len(values) - 1)
    frac = idx - lo
    return values[lo] * (1 - frac) + values[hi] * frac


def load_events(path: Path) -> Iterable[dict]:
    with path.open("r", encoding="utf-8") as f:
        for line_no, line in enumerate(f, 1):
            line = line.strip()
            if not line:
                continue
            try:
                yield json.loads(line)
            except json.JSONDecodeError as e:
                print(f"[warn] skip bad json line {line_no}: {e}")


def main() -> None:
    parser = argparse.ArgumentParser(description="Summarize websocket_multi metrics JSONL")
    parser.add_argument("input", type=Path, help="metrics jsonl file path")
    args = parser.parse_args()

    if not args.input.exists():
        raise SystemExit(f"input file not found: {args.input}")

    first_frame = []
    tts_stop = []
    event_counter = Counter()
    clients = set()

    for e in load_events(args.input):
        event = e.get("event", "unknown")
        event_counter[event] += 1
        if "client_index" in e:
            clients.add(e["client_index"])
        latency = e.get("latency_ms")
        if isinstance(latency, (int, float)):
            if event == "first_frame":
                first_frame.append(float(latency))
            elif event == "tts_stop":
                tts_stop.append(float(latency))

    first_frame.sort()
    tts_stop.sort()

    print("=== websocket_multi metrics summary ===")
    print(f"input: {args.input}")
    print(f"clients_seen: {len(clients)}")
    print(f"event_counts: {dict(event_counter)}")

    def print_stats(name: str, vals: List[float]) -> None:
        if not vals:
            print(f"{name}: no data")
            return
        print(
            f"{name}: count={len(vals)} avg={mean(vals):.2f}ms "
            f"p50={percentile(vals, 50):.2f}ms "
            f"p95={percentile(vals, 95):.2f}ms "
            f"p99={percentile(vals, 99):.2f}ms max={vals[-1]:.2f}ms"
        )

    print_stats("first_frame", first_frame)
    print_stats("tts_stop", tts_stop)

    # 简单成功率近似：有 first_frame 视作成功一次。
    # 若 tts_stop 远高于 first_frame，说明可能有重叠轮次或统计周期差异。
    if tts_stop:
        success_rate = (len(first_frame) / len(tts_stop)) * 100
        print(f"approx_success_rate(first_frame/tts_stop): {success_rate:.2f}%")


if __name__ == "__main__":
    main()
