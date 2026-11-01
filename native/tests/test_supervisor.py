"""Exercise the supervisor's low-battery exit and durable diagnostic paths."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SOURCE = (Path(__file__).parents[1] / 'package/urbancomp-ink/system/run.sh').read_text()


class Supervisor(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.data, self.runtime, self.bin = [self.root / n for n in ('data', 'runtime', 'bin')]
        for p in [self.data / 'state', self.runtime, self.bin]:
            p.mkdir(parents=True)
        self.env = dict(os.environ, PATH=str(self.bin) + ':' + os.environ['PATH'])

    def script(self, name, text):
        p = self.bin / name
        p.write_text('#!/bin/sh\n' + text + '\n')
        p.chmod(0o700)

    def run_functions(self, commands):
        a = SOURCE.index('archive_log() {')
        b = SOURCE.index('\narchive_log\n', a)
        text = f'D="{self.data}"\nR="{self.runtime}"\n'
        text += 'available=1\nstorage_available() { [ "$available" = 1 ]; }\n'
        text += SOURCE[a:b] + '\n' + commands
        result = subprocess.run(['/bin/sh'], input=text, env=self.env, capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_failed_starts_preserve_previous_real_session(self):
        (self.runtime / 'session.log').write_text('NATIVE_RUNNING app=123\nBattery at or below 20%\n')
        self.run_functions('archive_log')
        previous = (self.data / 'state/session.1.log').read_text()
        (self.runtime / 'session.log').write_text('Low battery: refused start\n')
        self.run_functions('archive_log\narchive_log')
        self.assertEqual((self.data / 'state/session.1.log').read_text(), previous)
        self.assertFalse((self.data / 'state/session.2.log').exists())

    def test_exit_is_retained_until_storage_returns(self):
        self.run_functions('''
available=0
reason=low_battery
battery=20
app=123
app_status=null
restore_status=0
record_exit
''')
        self.assertTrue((self.runtime / 'exit-pending').exists())
        self.assertFalse((self.data / 'state/last-exit.json').exists())
        self.run_functions('flush_exit\nflush_exit')
        data = json.loads((self.data / 'state/last-exit.json').read_text())
        self.assertEqual(data['reason'], 'low_battery')
        self.assertEqual(data['battery_percent'], 20)
        self.assertEqual(len((self.data / 'state/supervisor-events.jsonl').read_text().splitlines()), 1)

    def test_low_battery_start_records_reason_without_launching_app(self):
        (self.data / 'ENABLED').touch()
        (self.data / 'tested-driver.txt').write_text('fbink\n')
        (self.data / 'state/frame.png').write_bytes(b'cached frame')
        (self.data / 'ink-native').write_text('#!/bin/sh\ntouch "' + str(self.root / 'unexpected-launch') + '"\n')
        (self.runtime / 'restore.sh').write_text('#!/bin/sh\nexit 0\n')
        self.script('id', 'echo 0')
        self.script('timeout', 'shift\nexec "$@"')
        self.script('sleep', 'exit 0')
        self.script('lipc-get-prop', 'case "$2" in driveModeState) echo 0;; userstoreIsAvailable) echo 1;; battLevel) echo 20;; esac')
        mounts = self.root / 'mounts'
        mounts.write_text('fsp /mnt/us fuse rw,relatime 0 0\n')
        text = SOURCE.replace('/tmp/urbancomp-ink-runtime', str(self.runtime)).replace('/mnt/us/urbancomp-ink', str(self.data)).replace('/proc/mounts', str(mounts))
        result = subprocess.run(['/bin/sh'], input=text, env=self.env, text=True, capture_output=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse((self.root / 'unexpected-launch').exists())
        reason = json.loads((self.data / 'state/last-exit.json').read_text())
        self.assertEqual(reason['reason'], 'low_battery_start')
        self.assertEqual(reason['battery_percent'], 20)
        self.assertIsNone(reason['native_pid'])

    def tearDown(self):
        self.tmp.cleanup()


if __name__ == '__main__':
    unittest.main()
