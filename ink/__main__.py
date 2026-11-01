import argparse
import json
from pathlib import Path
from .content import API, get_json, adapt
from .publish import publish, validate
from .screen import PROFILES
from .server import serve


def main():
    parser = argparse.ArgumentParser(description='UrbanComp Ink: read-only research display')
    commands = parser.add_subparsers(dest='command', required=True)
    build = commands.add_parser('build')
    build.add_argument('--input', type=Path, help='Previously saved public dashboard JSON; omit to fetch')
    build.add_argument('--output', type=Path, default=Path('generated/pw2'))
    build.add_argument('--profile', choices=PROFILES, default='pw2')
    check = commands.add_parser('validate'); check.add_argument('folder', type=Path)
    web = commands.add_parser('serve'); web.add_argument('--host', default='127.0.0.1')
    web.add_argument('--port', default=8765, type=int)
    web.add_argument('--output', default=Path('generated'), type=Path)
    args = parser.parse_args()
    if args.command == 'build':
        raw = json.loads(args.input.read_text()) if args.input else get_json(API)
        folder = publish(adapt(raw), args.output, PROFILES[args.profile])
        print(folder)
    elif args.command == 'validate': print(json.dumps(validate(args.folder), ensure_ascii=False, indent=2))
    else: serve(args.output, args.host, args.port)


if __name__ == '__main__': main()
