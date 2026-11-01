"""Read-only adapter for the site's observed public dashboard, not its daily PDF."""
import hashlib
import html
import json
import re
import unicodedata
from datetime import datetime
from html.parser import HTMLParser
from urllib.parse import quote, urljoin, urlsplit
from urllib.request import Request, urlopen

ORIGIN = 'https://research.urbancomp.dev'
API = ORIGIN + '/api/dashboard'
MAX_BYTES = 2_000_000


class ContentError(ValueError):
    pass


class Plain(HTMLParser):
    def __init__(self):
        super().__init__(); self.parts = []; self.hidden = 0

    def handle_starttag(self, tag, attrs):
        if tag in ('script', 'style'): self.hidden += 1

    def handle_endtag(self, tag):
        if tag in ('script', 'style'): self.hidden = max(0, self.hidden - 1)

    def handle_data(self, data):
        if not self.hidden: self.parts.append(data)


def clean(value):
    if value is None: return ''
    if not isinstance(value, str): raise ContentError('TEXT_TYPE')
    if len(value) > 20000: raise ContentError('TEXT_TOO_LONG')
    p = Plain(); p.feed(value)
    return ' '.join(unicodedata.normalize('NFC', html.unescape(''.join(p.parts))).split())


def safe_url(value):
    if not isinstance(value, str): return None
    try:
        u = urlsplit(value)
        if u.scheme not in ('http', 'https') or not u.hostname or u.username or u.password:
            return None
        if any(ord(c) < 33 for c in value): return None
        return value
    except ValueError:
        return None


def canonical(value):
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode()


def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def get_json(url):
    # Only the two public dashboard URLs; no tokens, login cookies, or arbitrary URLs.
    if url not in (API, API + '?mode=version'): raise ContentError('URL_NOT_ALLOWED')
    req = Request(url, headers={'User-Agent': 'UrbanCompInk/0.2 (personal e-ink display)',
                               'Accept': 'application/json'})
    with urlopen(req, timeout=45) as r:
        if urlsplit(r.url).netloc != urlsplit(API).netloc:
            raise ContentError('UNEXPECTED_REDIRECT')
        data = r.read(MAX_BYTES + 1)
    if len(data) > MAX_BYTES: raise ContentError('RESPONSE_TOO_LARGE')
    result = json.loads(data)
    if not isinstance(result, dict): raise ContentError('SCHEMA_CHANGED')
    return result


def timestamp(value):
    if not isinstance(value, str): raise ContentError('TIMESTAMP_MISSING')
    parsed = datetime.fromisoformat(value.replace('Z', '+00:00'))
    if parsed.tzinfo is None: raise ContentError('TIMEZONE_MISSING')
    return value


def adapt(raw):
    for key in ('home', 'papers', 'digest', 'version', 'published_at', 'unavailable'):
        if key not in raw: raise ContentError('SCHEMA_CHANGED: ' + key)
    if raw['unavailable']: raise ContentError('UPSTREAM_PARTIAL: keeping previous snapshot')
    if not isinstance(raw['papers'], dict) or not isinstance(raw['home'], dict):
        raise ContentError('SCHEMA_CHANGED')
    notes = []
    papers = []
    seen = set()
    for item in raw['papers'].get('items', []):
        title = clean(item.get('translated_title') or item.get('title'))
        url = safe_url(item.get('source_url'))
        identifier = clean(item.get('id'))
        source = clean(item.get('source_name'))
        if not title or not url or not identifier or not source:
            notes.append('Skipped a paper with missing title, ID or source'); continue
        if identifier in seen or url in seen: continue
        seen.update((identifier, url))
        summary = clean(item.get('translated_summary') or item.get('summary'))
        # Publisher volume/issue metadata is not an abstract.
        if re.match(r'^(Volume\s+\d|Published(?:\s+date)?\s*:|出版日期[:：])', summary, re.I):
            summary = '摘要尚未就绪，可在原文查看。'
        papers.append({'id': identifier, 'title': title, 'summary': summary or '暂无摘要。',
                       'source': source, 'url': url,
                       'detail_url': ORIGIN + '/items/' + quote(identifier, safe=''),
                       'published_at': item.get('published_at'),
                       'ingested_at': item.get('created_at'), 'updated_at': item.get('updated_at')})
        if len(papers) == 6: break
    if not papers: raise ContentError('NO_VALID_PAPERS: keeping previous snapshot')
    cards = (raw['home'].get('daily_summary') or {}).get('cards', [])
    overview = next((c for c in cards if c.get('card_key') == 'overview'), None)
    focus = {'title': papers[0]['title'], 'summary': papers[0]['summary'],
             'date': None, 'source_ids': [papers[0]['id']], 'url': papers[0]['url']}
    if overview and overview.get('item_ids') and clean(overview.get('title')):
        focus = {'title': clean(overview['title']), 'summary': clean(overview.get('summary')),
                 'date': overview.get('date_key'), 'source_ids': overview['item_ids'],
                 'url': ORIGIN + '/'}
    radars = []
    for card in (raw.get('digest') or {}).get('cards', [])[:2]:
        if not card.get('item_ids') or not clean(card.get('direction_title')): continue
        radars.append({'title': clean(card['direction_title']),
                       'summary': clean(card.get('trend_summary')),
                       'date': card.get('date_key'), 'source_ids': card['item_ids'],
                       'url': ORIGIN + '/radar#direction-' + quote(card['direction_key'], safe='')})
    spark = None
    for card in (raw['home'].get('research_ideas') or {}).get('cards', []):
        qs = card.get('questions') or []
        if qs and card.get('item_ids'):
            spark = {'title': clean(qs[0].get('title')),
                     'question': clean(qs[0].get('question')),
                     'date': card.get('date_key'), 'source_ids': card['item_ids'],
                     'url': ORIGIN + '/ideas'}
            break
    dates = [p['updated_at'] for p in papers if p.get('updated_at')]
    if overview and overview.get('updated_at'): dates.append(overview['updated_at'])
    dates += [c['updated_at'] for c in (raw.get('digest') or {}).get('cards', [])[:2] if c.get('updated_at')]
    updated = max(dates, key=lambda d: datetime.fromisoformat(timestamp(d).replace('Z', '+00:00'))) if dates else raw['published_at']
    return {'schema_version': 2, 'kind': 'live-dashboard', 'source_url': API,
            'source_version': str(raw['version']),
            'source_updated_at': timestamp(updated), 'snapshot_published_at': timestamp(raw['published_at']),
            'focus': focus, 'radars': radars, 'papers': papers, 'spark': spark,
            'warnings': notes,
            'provenance_note': '公开首页快照；简报、雷达和论文各自更新，不代表同一期研究晨报。'}
