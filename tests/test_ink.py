import copy
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
from PIL import Image
from ink.content import adapt, ContentError, clean, safe_url
from ink.publish import publish, validate
from ink.screen import render
from ink.server import State, INTERVAL

FIXTURE = Path(__file__).parents[1] / 'fixtures/synthetic-dashboard.json'


class InkTests(unittest.TestCase):
    def setUp(self):
        self.raw = json.loads(FIXTURE.read_text())
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)

    def test_source_urls_dates_and_not_daily_issue(self):
        m = adapt(self.raw)
        self.assertEqual(m['kind'], 'live-dashboard')
        self.assertNotIn('issue_no', m)
        self.assertNotEqual(m['papers'][1]['published_at'], m['papers'][1]['ingested_at'])
        self.assertIn('摘要尚未就绪', m['papers'][0]['summary'])
        self.assertTrue(all(safe_url(p['url']) and p['id'] for p in m['papers']))

    def test_bad_sources_and_empty_feed_fail(self):
        self.raw['papers']['items'][0]['source_url'] = 'javascript:alert(1)'
        m = adapt(self.raw)
        self.assertEqual(len(m['papers']), 5)
        self.assertTrue(m['warnings'])
        self.raw['papers']['items'] = []
        with self.assertRaises(ContentError): adapt(self.raw)

    def test_partial_or_schema_drift_fails(self):
        self.raw['unavailable'] = ['papers']
        with self.assertRaises(ContentError): adapt(self.raw)
        del self.raw['home']
        with self.assertRaises(ContentError): adapt(self.raw)

    def test_duplicates_removed(self):
        self.raw['papers']['items'].insert(1, copy.deepcopy(self.raw['papers']['items'][0]))
        m = adapt(self.raw)
        self.assertEqual(len({p['id'] for p in m['papers']}), len(m['papers']))

    def test_plain_text_and_safe_links(self):
        self.assertEqual(clean('<script>bad()</script><b>城市 &amp; GeoAI</b>'), '城市 & GeoAI')
        for url in ('file:///tmp/x', 'https://a:b@example.org', 'https://x.test/\nx', 'javascript:x'):
            self.assertIsNone(safe_url(url))

    def assert_layout(self, result):
        boxes = result['text_boxes']
        for i, a in enumerate(boxes):
            x1,y1,x2,y2 = a['bbox']
            self.assertTrue(0 <= x1 <= x2 <= result['width'])
            self.assertTrue(0 <= y1 <= y2 <= result['height'])
            for b in boxes[i+1:]:
                xx1,yy1,xx2,yy2 = b['bbox']
                self.assertFalse(max(x1,xx1) < min(x2,xx2) and max(y1,yy1) < min(y2,yy2), (a,b))

    def test_native_browser_and_legacy_dimensions(self):
        for size in ((758,1024),(600,800),(758,900),(600,740),(400,500)):
            with self.subTest(size=size):
                out = self.root / 'screen.png'
                result = render(adapt(self.raw), out, size)
                self.assert_layout(result)
                with Image.open(out) as im:
                    self.assertEqual(im.size, size)
                    self.assertEqual(im.mode, 'L')
                    self.assertTrue(all(value % 17 == 0 for _,value in im.getcolors(256)))

    def test_long_mixed_titles(self):
        m = adapt(self.raw)
        title = '面向城市空间智能的跨区域泛化研究（GeoAI / 東京）' * 5
        m['focus']['title'] = title
        for p in m['papers']: p['title'] = title
        for r in m['radars']: r['title'] = title; r['summary'] = 'GeospatialFoundationModel' * 20
        result = render(m, self.root/'long.png')
        self.assert_layout(result)
        self.assertTrue(result['warnings'])
        self.assertEqual(m['focus']['title'], title)

    def test_sparse_modules(self):
        self.raw['digest']['cards'] = []
        self.raw['home']['daily_summary'] = None
        self.raw['papers']['items'] = self.raw['papers']['items'][:1]
        self.assert_layout(render(adapt(self.raw), self.root/'sparse.png'))

    def test_idempotent_and_ignore_unrelated_version(self):
        m = adapt(self.raw)
        first = publish(m, self.root)
        self.raw['version'] = 'only-evening-changed'
        self.raw['published_at'] = '2026-10-25T00:00:00+00:00'
        second = publish(adapt(self.raw), self.root)
        self.assertEqual(first, second)
        self.assertEqual(len(list((self.root/'editions').glob('[!.]*'))), 1)

    def test_failed_render_preserves_current(self):
        m = adapt(self.raw); first = publish(m, self.root)
        m['focus']['title'] += ' changed'
        with patch('ink.publish.render', side_effect=RuntimeError('disk or renderer failure')):
            with self.assertRaises(RuntimeError): publish(m, self.root)
        self.assertEqual((self.root/'current').resolve(), first)
        validate(first)

    def test_corrupt_artifact_detected(self):
        folder = publish(adapt(self.raw), self.root)
        (folder/'screen.png').write_bytes(b'broken')
        with self.assertRaises(ValueError): validate(folder)

    def test_30_minute_schedule_no_poll_on_each_request_and_offline_retains(self):
        calls = []; clock = [0]; offline = [False]
        def fetch(url):
            calls.append(url)
            if offline[0]: raise OSError('offline')
            return {'version':self.raw['version'],'running':False} if '?' in url else self.raw
        s = State(self.root, fetch=fetch, clock=lambda:clock[0], weather_fetch=lambda:None)
        s.refresh(); self.assertEqual(len(calls), 2)
        first = s.image((758,1024)); s.refresh(); self.assertEqual(len(calls),2)
        clock[0] = INTERVAL - 1; s.refresh(); self.assertEqual(len(calls),2)
        clock[0] = INTERVAL; s.refresh(); self.assertEqual(len(calls),3)
        offline[0] = True; clock[0] += INTERVAL; s.refresh()
        self.assertTrue(s.stale); self.assertEqual(first, s.image((758,1024)))
        offline[0] = False; clock[0] += INTERVAL; s.refresh(); self.assertFalse(s.stale)

    def test_html_full_titles_escaped_not_script(self):
        m = adapt(self.raw); m['papers'][0]['title'] = '<script>payload</script>'
        folder = publish(m, self.root)
        text = (folder/'reading.html').read_text()
        self.assertIn('&lt;script&gt;payload', text)
        self.assertNotIn('<script>', text)


if __name__ == '__main__': unittest.main()
