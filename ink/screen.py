"""Native-resolution newspaper typography; no browser required to render."""
import hashlib
import json
import os
import re
from datetime import datetime, timezone, timedelta
from functools import lru_cache
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

PROFILES = {'pw2': (758, 1024), 'k3': (600, 800)}
# Bundled OFL fonts make the historical browser prototype portable too.
_FONT_DIR = Path(__file__).resolve().parents[1] / 'native/package/urbancomp-ink/fonts'
FONTS = {
    kind: (os.environ.get('INK_FONT_' + kind.upper(), str(_FONT_DIR / name)),
           int(os.environ.get('INK_FONT_' + kind.upper() + '_INDEX', '0')))
    for kind, name in {
        'sans': 'SourceHanSansCN-Regular.otf',
        'bold': 'SourceHanSansCN-Regular.otf',
        'serif': 'SourceHanSerifCN-Regular.otf',
        'latin': 'SourceHanSerifCN-Regular.otf',
    }.items()
}


@lru_cache(maxsize=64)
def font(kind, size):
    path, index = FONTS[kind]
    try: return ImageFont.truetype(path, size, index=index)
    except OSError as e: raise ValueError('FONT_MISSING: configure INK_FONT_*') from e


def font_fingerprint():
    return {k: {'sha256': hashlib.sha256(Path(p).read_bytes()).hexdigest(), 'index': i}
            for k, (p, i) in FONTS.items()}


def wrap(text, f, width):
    # Measure actual glyph advances, preserving words unless wider than a line.
    tokens = re.findall(r'[A-Za-z0-9][A-Za-z0-9_./:+-]*|.', text)
    lines = []; line = ''
    for token in tokens:
        parts = list(token) if f.getlength(token) > width else [token]
        for part in parts:
            if line and f.getlength(line + part) > width:
                # Never start a line with closing punctuation, or end with an opener.
                move = ''
                if part[0] in '，。！？：；、）》」』】,.;:!?%）' or line[-1] in '（《「『【(':
                    move, line = line[-1], line[:-1]
                if line.strip(): lines.append(line.rstrip())
                line = move + part.lstrip()
            else: line += part
    if line.strip(): lines.append(line.rstrip())
    return lines


class Canvas:
    def __init__(self, width=758, height=1024):
        if not 400 <= width <= 1200 or not 500 <= height <= 1600:
            raise ValueError('VIEWPORT_OUT_OF_RANGE')
        self.w, self.h = width, height
        self.scale = min(width / 758, height / 1024)
        self.image = Image.new('L', (width, height), 255)
        self.draw = ImageDraw.Draw(self.image)
        self.boxes = []; self.warnings = []
        self.left = round(40 * self.scale)
        self.right = width - self.left

    def px(self, value): return round(value * self.scale)

    def text(self, value, x, y, size=26, kind='sans', width=None, lines=1, shade=0, label=''):
        f = font(kind, max(14, self.px(size)))
        x = round(x); y = round(y)
        available = width or (self.right - x)
        rows = wrap(value, f, available)
        if len(rows) > lines:
            rows = rows[:lines]
            while rows[-1] and f.getlength(rows[-1] + '…') > available: rows[-1] = rows[-1][:-1]
            rows[-1] += '…'
            self.warnings.append('Screen excerpt: ' + (label or value[:32]))
        step = self.px(size * 1.38)
        for row in rows:
            # Stop on missing glyphs rather than silently render squares.
            missing = f.getmask(chr(0x10ffff))
            for char in set(row) - {' ', '\t'}:
                mask = f.getmask(char)
                if mask.size == missing.size and bytes(mask) == bytes(missing):
                    raise ValueError('FONT_GLYPH_MISSING: ' + repr(char))
            box = self.draw.textbbox((x, y), row, font=f, anchor='lt')
            if box[0] < 0 or box[1] < 0 or box[2] > self.w or box[3] > self.h:
                raise ValueError('LAYOUT_OVERFLOW: ' + row)
            self.draw.text((x, y), row, font=f, anchor='lt', fill=shade)
            self.boxes.append({'label': label, 'text': row, 'bbox': list(box)})
            y += step
        return y

    def rule(self, y, heavy=False):
        self.draw.line((self.left, y, self.right, y), fill=0 if heavy else 153,
                       width=max(1, self.px(3 if heavy else 1)))

    def label(self, value, y, right=''):
        self.text(value, self.left, y, 18, 'bold', label='section')
        if right:
            f = font('sans', max(14, self.px(17)))
            self.text(right, self.right - f.getlength(right), y, 17, shade=68)


def render(model, path, size=(758, 1024)):
    c = Canvas(*size); p = c.px; x = c.left
    dt = datetime.fromisoformat(model['source_updated_at'].replace('Z', '+00:00'))
    dt = dt.astimezone(timezone(timedelta(hours=8)))
    c.label('URBANCOMP  /  INK', p(36), 'research.urbancomp.dev')
    c.rule(p(71), True)
    c.text('知新', x - p(2), p(91), 75, 'serif', width=p(420))
    c.text('研究前沿', c.right - p(171), p(111), 34, 'serif', width=p(175))
    c.label(dt.strftime('%Y.%m.%d') + '  /  ' + ('MON','TUE','WED','THU','FRI','SAT','SUN')[dt.weekday()], p(177), 'GEOAI · 城市 · 空间')
    c.rule(p(213), True)
    # Fixed vertical rhythm gives each module a strict budget; full text is retained separately.
    focus = model['focus']
    c.label('01  /  研究简报', p(236), str(focus.get('date') or '')[5:].replace('-', '.'))
    end = c.text(focus['title'], x, p(277), 40, 'serif', lines=2, label='focus-title')
    c.text(focus['summary'], x, end + p(24), 26,
           lines=3 if end < p(350) else 2, shade=34, label='focus-summary')
    c.rule(p(483))
    c.label('02  /  研究雷达', p(504))
    radars = model.get('radars', [])
    if radars:
        gap = p(29); col = (c.right - x - gap) // 2
        for i, item in enumerate(radars[:2]):
            xx = x + i * (col + gap)
            end = c.text(item['title'], xx, p(543), 27, 'bold', width=col, lines=2, label='radar-title')
            c.text(item['summary'], xx, end + p(16), 24, width=col,
                   lines=3 if end < p(600) else 2, shade=34, label='radar-summary')
        if len(radars) == 2:
            mid = x + col + gap // 2
            c.draw.line((mid, p(544), mid, p(686)), fill=187, width=1)
    else: c.text('暂无更新，保留已收录研究。', x, p(551), 26, shade=34)
    c.rule(p(714))
    c.label('03  /  值得精读', p(738))
    for i, item in enumerate(model['papers'][:2]):
        yy = p(779 + i * 76)
        c.text('%02d' % (i + 1), x, yy, 27, 'latin', width=p(40))
        c.text(item['title'], x + p(47), yy, 27, 'bold', lines=1, label='paper-title')
        c.text(item['source'], x + p(47), yy + p(39), 18, lines=1, shade=68, label='paper-source')
    # Never display wall-clock time as a claim that data just updated.
    c.rule(c.h - p(83), True)
    weather = model.get('weather')
    line = '武汉光谷  ·  天气暂不可用'
    if weather:
        line = '武汉光谷  ·  %s  %s℃' % (weather['condition'], weather['temperature'])
        if weather.get('stale'): line = '武汉光谷  ·  天气待更新'
    c.text(line, x, c.h - max(42, p(62)), 24, 'bold', width=c.right-x, label='weather')
    footer = dt.strftime('资料 %m.%d %H:%M')
    if weather:
        footer += '   /   天气 ' + weather['time'][11:16] + '   %s–%s℃' % (weather['low'], weather['high'])
    if model.get('kind') == 'synthetic': footer = '模拟样刊 · 非真实日报'
    c.text(footer, x, c.h - max(20, p(30)), 16, shade=68)
    c.image = c.image.point([round(i / 17) * 17 for i in range(256)])
    c.image.save(path, optimize=True)
    return {'width': c.w, 'height': c.h, 'mode': 'L', 'grayscale_levels': 16,
            'text_boxes': c.boxes, 'warnings': c.warnings}
