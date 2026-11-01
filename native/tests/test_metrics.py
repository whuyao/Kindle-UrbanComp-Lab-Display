import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('metrics', Path(__file__).parents[1] / 'analyze_metrics.py')
metrics = importlib.util.module_from_spec(spec)
spec.loader.exec_module(metrics)


def day():
    common = dict(schema=1, version='0.5.4', device=True, session='device-session')
    rows = []
    for i in range(97):
        rows.append(dict(common, kind='sample', elapsed_seconds=i * 900, battery_percent=100 - i // 4,
                         charging=False, usb_power=False, rss_kib=32000, hwm_kib=35000))
    for i in range(49):
        rows.append(dict(common, kind='refresh', elapsed_seconds=i * 1800 + 20,
                         start_elapsed_seconds=i * 1800, outcome='completed', update_ok=i != 8,
                         error='HTTP 502' if i == 8 else ''))
    return rows


class MetricsTests(unittest.TestCase):
    def test_session_stability_is_separate_from_discharge(self):
        rows = day()
        for r in rows:
            if r['kind'] == 'sample':
                r['usb_power'] = r['elapsed_seconds'] < 43200
                r['charging'] = r['usb_power']
                r['process_cpu_ticks'] = r['elapsed_seconds']
                r['system_cpu_ticks'] = r['elapsed_seconds'] * 100
                r['frontlight_level'] = 0
        result = metrics.analyze(rows)
        s = result['sessions'][0]
        self.assertEqual(s['observed_session_hours'], 24)
        self.assertEqual(s['continuous_discharge_hours'], 12)
        self.assertEqual(s['app_cpu_share_percent'], 1)
        self.assertEqual(s['frontlight_levels_observed'], [0])
        self.assertEqual(s['session_refresh_attempts'], 49)
        self.assertFalse(result['observation_24h_complete'])

    def test_whole_day_success_rate_and_consumption(self):
        report = metrics.analyze(day())
        self.assertTrue(report['observation_24h_complete'])
        s = report['sessions'][0]
        self.assertEqual(s['battery_drop_percentage_points'], 24)
        self.assertEqual(s['refresh_attempts'], 49)
        self.assertEqual(s['refresh_successes'], 48)
        self.assertEqual(s['memory_high_water_kib'], 35000)
        self.assertIsNone(s['battery_life_estimate_days'])

    def test_host_charging_and_unknown_cannot_prove_discharge(self):
        for changes in ({'device': False}, {'charging': True}, {'usb_power': True}, {'usb_power': None}):
            rows = [dict(r, **changes) for r in day()]
            self.assertFalse(metrics.analyze(rows)['observation_24h_complete'])

    def test_restarts_and_sampling_gaps_cannot_make_a_day(self):
        rows = day()
        for r in rows:
            if r['elapsed_seconds'] >= 43200:
                r['session'] = 'restarted'
        self.assertFalse(metrics.analyze(rows)['observation_24h_complete'])
        rows = [r for r in day() if not (r['kind'] == 'sample' and 35000 < r['elapsed_seconds'] < 45000)]
        self.assertFalse(metrics.analyze(rows)['observation_24h_complete'])

    def test_short_run_and_missing_refreshes_remain_pending(self):
        rows = [r for r in day() if r['elapsed_seconds'] < 86399]
        self.assertFalse(metrics.analyze(rows)['observation_24h_complete'])
        rows = [r for r in day() if r['kind'] != 'refresh' or r['start_elapsed_seconds'] != 43200]
        self.assertFalse(metrics.analyze(rows)['observation_24h_complete'])

    def test_display_failure_does_not_pass(self):
        rows = day()
        rows.append(dict(rows[0], kind='display_failure', elapsed_seconds=1000))
        self.assertFalse(metrics.analyze(rows)['observation_24h_complete'])


if __name__ == '__main__':
    unittest.main()
