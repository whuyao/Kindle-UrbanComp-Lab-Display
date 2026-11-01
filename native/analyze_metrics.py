#!/usr/bin/env python3
"""Summarize device telemetry without extrapolating battery life from short runs."""
import argparse
import json
import math
from pathlib import Path


def number(value):
    return type(value) in (int, float) and math.isfinite(value)


def read_events(directory):
    events, invalid = [], 0
    for name in ('metrics.jsonl.previous', 'metrics.jsonl'):
        path = Path(directory) / name
        if not path.exists():
            continue
        for line in path.read_text(errors='replace').splitlines():
            try:
                event = json.loads(line)
                if not isinstance(event, dict):
                    raise ValueError('not an object')
                events.append(event)
            except (ValueError, TypeError):
                invalid += 1
    return events, invalid


def discharge_sample(row):
    return (type(row.get('battery_percent')) is int
            and 0 <= row['battery_percent'] <= 100
            and row.get('charging') is False and row.get('usb_power') is False
            and number(row.get('rss_kib')) and row['rss_kib'] > 0
            and number(row.get('hwm_kib')) and row['hwm_kib'] > 0)


def session_report(session, rows):
    rows = sorted(rows, key=lambda r: r['elapsed_seconds'])
    samples = [r for r in rows if r.get('kind') == 'sample']
    all_updates = [r for r in rows if r.get('kind') == 'refresh' and r.get('outcome') != 'cancelled']
    best, current = [], []
    for row in samples:
        if not discharge_sample(row):
            current = []
            continue
        if current:
            gap = row['elapsed_seconds'] - current[-1]['elapsed_seconds']
            if not 0 < gap <= 1200 or row['battery_percent'] > current[-1]['battery_percent']:
                current = []
        current.append(row)
        span = current[-1]['elapsed_seconds'] - current[0]['elapsed_seconds']
        best_span = best[-1]['elapsed_seconds'] - best[0]['elapsed_seconds'] if best else -1
        if span > best_span:
            best = current.copy()
    start = best[0]['elapsed_seconds'] if best else 0
    end = best[-1]['elapsed_seconds'] if best else 0
    updates = [r for r in rows if r.get('kind') == 'refresh'
               and r.get('outcome') != 'cancelled'
               and number(r.get('start_elapsed_seconds'))
               and start - 60 <= r['start_elapsed_seconds'] <= end + 60]
    successes = sum(r.get('update_ok') is True for r in updates)
    starts = sorted(r['start_elapsed_seconds'] for r in updates)
    gaps = [b - a for a, b in zip(starts, starts[1:])]
    max_gap = max(gaps, default=None)
    coverage = (bool(starts) and starts[0] <= start + 2100
                and starts[-1] >= end - 2100 and (max_gap is None or max_gap <= 2100))
    duration = end - start
    cpu = []
    for a, b in zip(best, best[1:]):
        keys = ('process_cpu_ticks', 'system_cpu_ticks')
        if all(number(r.get(k)) for r in (a,b) for k in keys):
            own = b[keys[0]] - a[keys[0]]
            total = b[keys[1]] - a[keys[1]]
            if 0 <= own <= total and total > 0:
                cpu.append((own,total))
    display_errors = [r for r in rows if r.get('kind') == 'display_failure' and start <= r['elapsed_seconds'] <= end]
    missing = []
    if duration < 86400:
        missing.append('尚无同一次运行中连续 24 小时的放电与内存样本')
    if not coverage or len(updates) < 48:
        missing.append('半小时更新记录尚未覆盖完整一天')
    if display_errors:
        missing.append('存在显示失败，需要检查')
    return {
        'session': session,
        'version': rows[-1].get('version'),
        'sample_count': len(samples),
        'observed_session_hours': round((samples[-1]['elapsed_seconds'] - samples[0]['elapsed_seconds']) / 3600, 4) if samples else 0,
        'session_refresh_attempts': len(all_updates),
        'session_refresh_successes': sum(r.get('update_ok') is True for r in all_updates),
        'eligible_discharge_sample_count': len(best),
        'continuous_discharge_hours': round(duration / 3600, 4),
        'first_sample_at': best[0].get('at') if best else None,
        'last_sample_at': best[-1].get('at') if best else None,
        'battery_start_percent': best[0]['battery_percent'] if best else None,
        'battery_end_percent': best[-1]['battery_percent'] if best else None,
        'battery_drop_percentage_points': best[0]['battery_percent'] - best[-1]['battery_percent'] if best else None,
        'app_cpu_share_percent': round(100 * sum(a for a,b in cpu) / sum(b for a,b in cpu), 3) if cpu else None,
        'frontlight_levels_observed': sorted({r['frontlight_level'] for r in best if type(r.get('frontlight_level')) is int}),
        'goroutines_peak': max((r['goroutines'] for r in best if type(r.get('goroutines')) is int), default=None),
        'rss_start_kib': best[0]['rss_kib'] if best else None,
        'rss_end_kib': best[-1]['rss_kib'] if best else None,
        'rss_peak_kib': max((r['rss_kib'] for r in best), default=None),
        'memory_high_water_kib': max((r['hwm_kib'] for r in best), default=None),
        'refresh_attempts': len(updates),
        'refresh_successes': successes,
        'refresh_success_percent': round(100 * successes / len(updates), 2) if updates else None,
        'max_refresh_gap_seconds': max_gap,
        'refresh_failures': [{'at': r.get('at'), 'error': r.get('error', 'unknown')} for r in updates if r.get('update_ok') is not True],
        'display_failures': len(display_errors),
        'observation_24h_complete': not missing,
        'remaining': missing,
        'battery_life_estimate_days': None,
    }


def analyze(events, version='0.5.4'):
    groups = {}
    for r in events:
        if (r.get('device') is not True or r.get('schema') != 1
                or r.get('version') != version or not r.get('session')
                or not number(r.get('elapsed_seconds')) or r['elapsed_seconds'] < 0):
            continue
        groups.setdefault(r['session'], []).append(r)
    sessions = [session_report(key, rows) for key, rows in groups.items()]
    sessions.sort(key=lambda r: r['continuous_discharge_hours'], reverse=True)
    return {
        'required_version': version,
        'device_sessions': len(sessions),
        'observation_24h_complete': any(s['observation_24h_complete'] for s in sessions),
        'sessions': sessions,
        'notes': '只统计实机记录；充电、外接电源、未知电源状态、采样缺口及进程重启会分开计算，不合并成连续一天。不外推续航天数。',
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('state_directory')
    parser.add_argument('--version', default='0.5.4')
    parser.add_argument('--output')
    args = parser.parse_args()
    events, invalid = read_events(args.state_directory)
    report = analyze(events, args.version)
    report['invalid_lines'] = invalid
    text = json.dumps(report, ensure_ascii=False, indent=2) + '\n'
    if args.output:
        Path(args.output).write_text(text)
    print(text, end='')


if __name__ == '__main__':
    main()
