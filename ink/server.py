"""A narrow read-only LAN viewer. No arbitrary file serving or remote commands."""
import ipaddress
import copy
import json
import threading
import time
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlsplit
from .content import API, get_json, adapt, canonical
from .publish import publish, validate
from .weather import get_weather

INTERVAL = 1800


class State:
    def __init__(self, root, fetch=get_json, clock=time.monotonic, weather_fetch=get_weather):
        self.root = Path(root).resolve(); self.fetch = fetch; self.clock = clock
        self.lock = threading.RLock(); self.last_check = None
        self.weather_fetch = weather_fetch
        self.source_version = None; self.model = None; self.stale = False
        self.error = None; self.checked_at = None; self.sizes = {}; self.clients = {}
        previous = self.root / 'pw2' / 'current'
        if previous.exists():
            try:
                validate(previous)
                self.model = json.loads((previous / 'content.json').read_text())
                self.source_version = self.model.get('source_version')
            except (OSError, ValueError, KeyError): pass

    def refresh(self, force=False):
        with self.lock:
            now = self.clock()
            if not force and self.last_check is not None and now - self.last_check < INTERVAL:
                return
            self.last_check = now
            self.checked_at = datetime.now(timezone.utc).isoformat()
            try:
                version = self.fetch(API + '?mode=version')
                if 'version' not in version or 'running' not in version:
                    raise ValueError('VERSION_SCHEMA_CHANGED')
                if version['running']:
                    raise ValueError('SOURCE_BUSY')
                if self.model is None or version['version'] != self.source_version:
                    raw = self.fetch(API)
                    candidate = adapt(raw)
                else:
                    candidate = copy.deepcopy(self.model)
                self.stale = False; self.error = None
            except Exception as exc:
                self.stale = True
                self.error = type(exc).__name__ + ': ' + str(exc)[:160]
                candidate = copy.deepcopy(self.model)
            if candidate:
                try: candidate['weather'] = self.weather_fetch()
                except Exception:
                    previous_weather = (self.model or {}).get('weather')
                    candidate['weather'] = copy.deepcopy(previous_weather)
                    if candidate['weather']: candidate['weather']['stale'] = True
                try:
                    location = publish(candidate, self.root / 'pw2')
                    self.model = candidate; self.source_version = candidate['source_version']
                    self.sizes = {(758, 1024): location}
                except Exception as exc:
                    self.stale = True; self.error = type(exc).__name__ + ': ' + str(exc)[:160]
            self.root.mkdir(parents=True, exist_ok=True)
            # Diagnostic state is private, not exposed through arbitrary directory serving.
            (self.root / 'health.json').write_bytes(canonical({'checked_at': self.checked_at,
                    'stale': self.stale, 'error': self.error}))

    def image(self, size):
        with self.lock:
            if self.model is None: raise ValueError('NO_SNAPSHOT_YET')
            if size not in self.sizes:
                # Bound the number of cached viewport variants.
                if len(self.sizes) >= 8: raise ValueError('TOO_MANY_VIEWPORTS')
                self.sizes[size] = publish(self.model, self.root / ('%sx%s' % size), size)
            return self.sizes[size]


def handler(state, host, port):
    class Handler(BaseHTTPRequestHandler):
        server_version = 'UrbanCompInk/0.2'

        def reply(self, status, data, mime='application/json', etag=None):
            if isinstance(data, str): data = data.encode('utf-8')
            if etag and self.headers.get('If-None-Match') == '"' + etag + '"':
                self.send_response(304); self.send_header('ETag', '"' + etag + '"'); self.end_headers(); return
            self.send_response(status)
            self.send_header('Content-Type', mime)
            self.send_header('Content-Length', str(len(data)))
            self.send_header('Cache-Control', 'no-cache')
            self.send_header('X-Content-Type-Options', 'nosniff')
            self.send_header('Referrer-Policy', 'no-referrer')
            if etag: self.send_header('ETag', '"' + etag + '"')
            self.end_headers(); self.wfile.write(data)

        def do_GET(self):
            # Bind to one local address. Reject foreign Host headers / cross-site fetches.
            if self.headers.get('Host') not in (host + ':' + str(port), host):
                return self.reply(403, b'{}')
            address = ipaddress.ip_address(self.client_address[0])
            if not (address.is_private or address.is_loopback): return self.reply(403, b'{}')
            if self.headers.get('Sec-Fetch-Site') == 'cross-site': return self.reply(403, b'{}')
            parsed = urlsplit(self.path)
            if parsed.path not in ('/', '/status', '/screen.png', '/reading.html', '/manifest.json'):
                return self.reply(404, b'{}')
            try:
                q = parse_qs(parsed.query)
                size = (int(q.get('w', ['758'])[0]), int(q.get('h', ['1024'])[0]))
                if not 400 <= size[0] <= 1200 or not 500 <= size[1] <= 1600:
                    raise ValueError('INVALID_VIEWPORT')
                if parsed.path == '/':
                    return self.reply(200, Path(__file__).with_name('viewer.html').read_bytes(), 'text/html; charset=utf-8')
                if parsed.path == '/status':
                    state.refresh()
                    location = state.image(size)
                    manifest = json.loads((location / 'manifest.json').read_text())
                    client = {'address': self.client_address[0], 'viewport': size,
                              'user_agent': self.headers.get('User-Agent', '')[:200],
                              'last_seen': datetime.now(timezone.utc).isoformat()}
                    state.clients[self.client_address[0]] = client
                    (state.root / 'clients.json').write_bytes(canonical(list(state.clients.values())))
                    return self.reply(200, canonical({'version': manifest['version'],
                            'stale': state.stale, 'checked_at': state.checked_at,
                            'source_updated_at': state.model['source_updated_at'], 'interval': INTERVAL}))
                location = state.image(size)
                manifest = json.loads((location / 'manifest.json').read_text())
                if parsed.path == '/screen.png':
                    requested = q.get('v', [manifest['version']])[0]
                    if requested != manifest['version']:
                        # Prevent a race between a status response and a newer successful build.
                        if len(requested) != 24 or any(c not in '0123456789abcdef' for c in requested):
                            return self.reply(400, b'{}')
                        older = location.parent / requested
                        if not older.exists(): return self.reply(409, b'{}')
                        location = older; manifest = validate(location)
                    return self.reply(200, (location / 'screen.png').read_bytes(), 'image/png', manifest['artifacts']['screen.png'])
                if parsed.path == '/reading.html':
                    return self.reply(200, (location / 'reading.html').read_bytes(), 'text/html; charset=utf-8')
                return self.reply(200, (location / 'manifest.json').read_bytes())
            except (ValueError, KeyError, OSError) as exc:
                self.log_error('%s', type(exc).__name__ + ': ' + str(exc)[:120])
                return self.reply(503, b'{"available":false}')

        def log_message(self, fmt, *args):
            print(self.log_date_time_string(), self.client_address[0], fmt % args, flush=True)
    return Handler


def serve(root, host, port):
    ip = ipaddress.ip_address(host)
    if not (ip.is_private or ip.is_loopback) or ip.is_unspecified:
        raise ValueError('Use the Mac LAN address or 127.0.0.1, never 0.0.0.0/public IP')
    state = State(root)
    state.refresh(force=True)
    server = ThreadingHTTPServer((host, port), handler(state, host, port))
    import os
    (Path(root) / 'service.pid').write_text(str(os.getpid()))
    print('UrbanComp Ink: http://%s:%s/ ; checks every 30 minutes while viewer connected' % (host, port), flush=True)
    if state.error: print('Initial update:', state.error, flush=True)
    try: server.serve_forever()
    finally: server.server_close()
