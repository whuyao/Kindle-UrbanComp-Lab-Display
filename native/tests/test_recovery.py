"""Exercise recovery against disposable child processes and mocked device files.

This verifies rollback logic on the host, not Kindle hardware compatibility.
No real Kindle settings or system paths are changed by this test.
"""
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import unittest

SYSTEM = Path(__file__).resolve().parents[1] / 'package/urbancomp-ink/system'


class Recovery(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.runtime = self.root / 'runtime'
        self.runtime.mkdir()
        self.bin = self.root / 'bin'
        self.bin.mkdir()
        self.log = self.root / 'calls'
        self.env = dict(os.environ, PATH=f'{self.bin}:' + os.environ['PATH'], CALLS=str(self.log))
        self.children = []
        self.script('timeout', '#!/bin/sh\nshift\nexec "$@"\n')
        self.script('initctl', '#!/bin/sh\necho "framework start/running"\n')
        self.script('lipc-set-prop', '#!/bin/sh\necho "$*" >> "$CALLS"\n[ "${FAIL_SET:-0}" = 0 ]\n')
        self.script('lipc-get-prop', '#!/bin/sh\ncase "$2" in driveModeState) echo "${DRIVE_MODE:-1}";; userstoreIsAvailable) echo "${STORE_AVAILABLE:-0}";; esac\n')
        self.proc = self.root / 'proc'
        self.proc.mkdir()
        self.usb = self.root / 'usb'
        self.usb.write_text('1\n')  # Recovery must not draw over USB mode.
        text = (SYSTEM / 'restore.sh').read_text()
        text = text.replace('/tmp/urbancomp-ink-runtime', str(self.runtime))
        text = text.replace('/mnt/us/urbancomp-ink', str(self.root / 'data'))
        text = text.replace('/proc/', str(self.proc) + '/')
        text = text.replace('/sys/devices/platform/fsl-usb2-udc/gadget/connected', str(self.usb))
        self.restore = self.root / 'restore.sh'
        self.restore.write_text(text)

    def script(self, name, text):
        p = self.bin / name
        p.write_text(text)
        p.chmod(0o700)

    def child(self, label, paused=False):
        p = subprocess.Popen(['sleep', '60'])
        self.children.append(p)
        d = self.proc / str(p.pid)
        d.mkdir()
        fields = [str(p.pid), '(test)', 'S'] + ['0'] * 18 + ['12345']
        (d / 'stat').write_text(' '.join(fields))
        (self.runtime / f'{label}.pid').write_text(str(p.pid))
        (self.runtime / f'{label}.start').write_text('12345')
        if paused:
            os.kill(p.pid, signal.SIGSTOP)
            _, status = os.waitpid(p.pid, os.WUNTRACED)
            self.assertTrue(os.WIFSTOPPED(status))
            (self.runtime / 'cvm-paused').touch()
        return p

    def state(self, p):
        pid, status = os.waitpid(p.pid, os.WNOHANG | os.WCONTINUED | os.WUNTRACED)
        return 'S' if pid and os.WIFCONTINUED(status) else 'T'

    def run_restore(self):
        return subprocess.run(['/bin/sh', str(self.restore)], env=self.env, capture_output=True, text=True, timeout=10)

    def test_restore_resumes_our_ui_and_original_power(self):
        cvm = self.child('cvm', paused=True)
        self.child('app')
        (self.runtime / 'pillow-disabled').touch()
        (self.runtime / 'power-changed').touch()
        (self.runtime / 'power-before').write_text('1')
        (self.runtime / 'redraw-home').touch()
        result = self.run_restore()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn('T', self.state(cvm))
        calls = self.log.read_text()
        self.assertIn('preventScreenSaver 1', calls)
        self.assertIn('disableEnablePillow enable', calls)
        self.assertNotIn('appmgrd', calls)
        self.assertEqual(self.run_restore().returncode, 0)
        self.assertEqual(self.log.read_text(), calls, 'second recovery should do nothing')

    def test_reused_pid_is_not_resumed(self):
        cvm = self.child('cvm', paused=True)
        (self.runtime / 'cvm.start').write_text('other-start-time')
        self.assertEqual(self.run_restore().returncode, 0)
        self.assertIn('T', self.state(cvm))

    def test_failed_restore_remains_retryable(self):
        (self.runtime / 'pillow-disabled').touch()
        (self.runtime / 'power-changed').touch()
        (self.runtime / 'power-before').write_text('0')
        self.env['FAIL_SET'] = '1'
        self.assertEqual(self.run_restore().returncode, 1)
        self.assertTrue((self.runtime / 'restore-failed').exists())
        self.assertTrue((self.runtime / 'power-changed').exists())
        self.env['FAIL_SET'] = '0'
        self.assertEqual(self.run_restore().returncode, 0)
        self.assertFalse((self.runtime / 'restore-failed').exists())
        self.assertFalse((self.runtime / 'power-changed').exists())

    def test_storage_guard_uses_volume_state_not_stale_cable_flag(self):
        data = self.root / 'data'
        data.mkdir()
        (data / 'ENABLED').touch()
        mounts = self.proc / 'mounts'
        mounts.write_text('fsp /mnt/us fuse rw,relatime 0 0\n')
        source = (SYSTEM / 'run.sh').read_text()
        start = source.index('storage_available() {')
        end = source.index('\nsave_log()', start)
        guard = source[start:end].replace('/proc/mounts', str(mounts))
        script = self.root / 'guard.sh'
        script.write_text('D="' + str(data) + '"\nbounded() { "$@"; }\n' + guard + '\nstorage_ok\n')
        def check(drive, available, success):
            env = dict(self.env, DRIVE_MODE=drive, STORE_AVAILABLE=available)
            r = subprocess.run(['/bin/sh', str(script)], env=env, capture_output=True, timeout=5)
            self.assertEqual(r.returncode == 0, success, r.stderr)
        # Reproduces the device evidence: cable flag 1, volume mode 0, internal store 1.
        self.assertEqual(self.usb.read_text().strip(), '1')
        check('0', '1', True)
        check('1', '1', False)
        check('0', '0', False)
        check('', '1', False)
        mounts.write_text('fsp /mnt/us fuse ro,relatime 0 0\n')
        check('0', '1', False)
        mounts.write_text('fsp /mnt/us fuse rw,relatime 0 0\n')
        (data / 'DISABLED').touch()
        check('0', '1', False)

    def test_frontlight_restore_is_retryable_and_idempotent(self):
        (self.runtime / 'light-before').write_text('18')
        (self.runtime / 'light-changed').touch()
        self.env['FAIL_SET'] = '1'
        self.assertEqual(self.run_restore().returncode, 1)
        self.assertTrue((self.runtime / 'light-changed').exists())
        self.env['FAIL_SET'] = '0'
        self.assertEqual(self.run_restore().returncode, 0)
        self.assertFalse((self.runtime / 'light-changed').exists())
        calls = self.log.read_text()
        self.assertIn('flIntensity 18', calls)
        self.assertEqual(self.run_restore().returncode, 0)
        self.assertEqual(self.log.read_text(), calls)

    def tearDown(self):
        for p in self.children:
            if p.poll() is None:
                os.kill(p.pid, signal.SIGCONT)
                p.terminate()
            p.wait(timeout=3)
        self.tmp.cleanup()


if __name__ == '__main__':
    unittest.main()
