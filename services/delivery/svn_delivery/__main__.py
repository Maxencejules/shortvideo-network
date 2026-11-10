"""Loopback commands for the origin/test adapter. Nothing here is a public deploy."""

from __future__ import annotations

import argparse
import json
import signal
import sys
from pathlib import Path

from svn_delivery.catalog import Catalog
from svn_delivery.errors import DeliveryError
from svn_delivery.ids import ADAPTER_NAME, ADAPTER_VERSION, CHAIN_NOTES
from svn_delivery.server import MediaServer


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="svn_delivery")
    commands = parser.add_subparsers(dest="command", required=True)

    note = commands.add_parser("note-chain")
    note.add_argument("--root", type=Path, required=True)
    note.add_argument("--note", choices=CHAIN_NOTES, required=True)

    grant = commands.add_parser("authorize")
    grant.add_argument("--root", type=Path, required=True)
    grant.add_argument("--content", required=True)
    grant.add_argument("--seconds", type=float, required=True)

    serve = commands.add_parser("serve")
    serve.add_argument("--root", type=Path, required=True)
    serve.add_argument("--bind", default="127.0.0.1")
    serve.add_argument("--port", type=int, default=8443)

    remove = commands.add_parser("remove")
    remove.add_argument("--root", type=Path, required=True)
    remove.add_argument("--content", required=True)

    args = parser.parse_args(argv)
    try:
        catalog = Catalog(args.root)
        if args.command == "note-chain":
            catalog.note_chain(args.note)
            print(args.note)
            return 0
        if args.command == "authorize":
            granted = catalog.authorize(args.content, seconds=args.seconds)
            print(granted.manifest_sha256)
            return 0
        if args.command == "remove":
            catalog.remove(args.content)
            print("REMOVED")
            return 0
        server = MediaServer(catalog, bind=args.bind, port=args.port)
        server.start()
        print(
            json.dumps(
                {
                    "adapter": ADAPTER_NAME,
                    "adapter_version": ADAPTER_VERSION,
                    "origin": f"https://{args.bind}:{server.port}",
                    "ca": str(server.cert.cert_path),
                    "hosting_promise": False,
                },
                sort_keys=True,
            ),
            flush=True,
        )
        signal.signal(signal.SIGINT, lambda *_args: sys.exit(0))
        signal.pause()
    except DeliveryError as exc:
        print(exc.code, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
