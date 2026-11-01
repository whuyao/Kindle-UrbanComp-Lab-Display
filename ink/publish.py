"""Versioned local output. Validate before switching the current symlink."""
import hashlib
import html
import json
import os
import tempfile
import threading
from pathlib import Path
from PIL import Image
from .content import canonical, digest
from .screen import render, font_fingerprint

LOCK = threading.RLock()


def sha(path): return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def reading_page(model):
    e = html.escape
    sections = []
    for name, rows in [('研究简报', [model['focus']]), ('研究雷达', model['radars']), ('值得精读', model['papers'])]:
        items = []
        for row in rows:
            url = row.get('url') or model['source_url']
            detail = row.get('detail_url')
            links = '<a href="' + e(url, quote=True) + '">原始来源</a>'
            if detail: links += ' · <a href="' + e(detail, quote=True) + '">平台详情</a>'
            date = row.get('date') or (row.get('ingested_at') or '')[:10]
            items.append('<article><h2>' + e(row['title']) + '</h2><small>' +
                         e((row.get('source') or 'UrbanComp Research') + ' · ' + (date or '')) +
                         '</small><p>' + e(row['summary']) + '</p><p>' + links + '</p></article>')
        sections.append('<section><h1>' + name + '</h1>' + ''.join(items) + '</section>')
    weather = model.get('weather')
    weather_credit = ('<p>天气：<a href="https://open-meteo.com/">Open-Meteo</a>，光谷广场附近网格预报，非站点实测。时间：'
                      + e(weather['time']) + '</p>') if weather else ''
    return ('<!doctype html><html lang="zh-CN"><meta charset="utf-8">'
            '<meta name="viewport" content="width=device-width,initial-scale=1">'
            '<title>UrbanComp Ink · 阅读</title><style>'
            'body{max-width:44em;margin:0 auto;padding:24px;color:#111;background:#fff;font:22px/1.8 sans-serif}'
            'h1{border-top:3px solid;padding-top:24px;font-size:1.3em}h2{font-size:1.15em;line-height:1.6}'
            'article{padding-bottom:24px;border-bottom:1px solid #aaa}small{font-size:.8em}'
            'a{color:#111;text-decoration:underline}p{overflow-wrap:break-word}</style>'
            '<a href="/">← 返回信息屏</a><p>URBANCOMP / INK</p><p>' + e(model['provenance_note']) + '</p>'
            + ''.join(sections) + weather_credit + '<footer>基于 UrbanComp Research 公开内容整理；摘要可能含 AI 辅助整理，以原文为准。</footer></html>')


def visible_content(model):
    # Neither fetch time, statistics, unrelated nightly stories nor an opaque API version changes the screen.
    return {k: model.get(k) for k in ('kind', 'source_updated_at', 'focus', 'radars', 'papers', 'weather')}


def publish(model, root, size=(758, 1024)):
    root = Path(root).resolve()
    with LOCK:
        root.mkdir(parents=True, exist_ok=True)
        code_hash = hashlib.sha256(b''.join(p.read_bytes() for p in sorted(Path(__file__).parent.glob('*.py')))).hexdigest()
        key = digest({'content': visible_content(model), 'size': size,
                      'fonts': font_fingerprint(), 'code': code_hash})[:24]
        edition = root / 'editions' / key
        if edition.exists():
            validate(edition)
        else:
            edition.parent.mkdir(exist_ok=True)
            staging = Path(tempfile.mkdtemp(prefix='.build-', dir=edition.parent))
            layout = render(model, staging / 'screen.png', size)
            (staging / 'reading.html').write_text(reading_page(model), encoding='utf-8')
            # Detailed normalized provenance stays outside the HTTP allowlist.
            (staging / 'content.json').write_bytes(canonical(model))
            (staging / 'layout.json').write_bytes(canonical(layout))
            manifest = {'schema_version': 2, 'version': key, 'kind': model['kind'],
                        'source_updated_at': model['source_updated_at'], 'check_interval_seconds': 1800,
                        'width': size[0], 'height': size[1],
                        'artifacts': {f: sha(staging / f) for f in ('screen.png', 'reading.html')},
                        'warnings': model.get('warnings', []) + layout['warnings']}
            (staging / 'manifest.json').write_bytes(canonical(manifest))
            validate(staging)
            os.rename(staging, edition)
        pointer = root / ('.current-' + key)
        pointer.unlink(missing_ok=True)
        pointer.symlink_to(edition.relative_to(root), target_is_directory=True)
        os.replace(pointer, root / 'current')
        return edition


def validate(folder):
    folder = Path(folder)
    manifest = json.loads((folder / 'manifest.json').read_text())
    for name, expected in manifest['artifacts'].items():
        if name not in ('screen.png', 'reading.html') or sha(folder / name) != expected:
            raise ValueError('ARTIFACT_HASH_MISMATCH')
    with Image.open(folder / 'screen.png') as image:
        image.load()
        if image.mode != 'L' or image.size != (manifest['width'], manifest['height']):
            raise ValueError('INVALID_IMAGE')
        if any(v % 17 for _, v in image.getcolors(256)):
            raise ValueError('INVALID_GRAYSCALE')
    return manifest
