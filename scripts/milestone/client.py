#!/usr/bin/env python3
"""Read committed metadata and fetch media over the public HTTPS routes.

This client does not open a database, a key file, or a quarantine directory.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import ssl
import urllib.request


def main() -> int:
    parser = argparse.ArgumentParser(prog="milestone-client")
    parser.add_argument("--indexer", required=True)
    parser.add_argument("--content", required=True)
    parser.add_argument("--delivery", required=True)
    parser.add_argument("--ca", required=True)
    args = parser.parse_args()
    indexer = json.load(
        urllib.request.urlopen(args.indexer.rstrip("/") + "/v1/content", timeout=10)
    )
    rows = [row for row in indexer.get("content", []) if row.get("ID") == args.content]
    if len(rows) != 1:
        print(f"CLIENT metadata_rows={len(rows)}")
        return 1
    row = rows[0]
    print(
        f"CLIENT metadata content={row.get('ID')} version={row.get('Version')} "
        f"withdrawn={row.get('Withdrawn')} manifest={row.get('Manifest')}"
    )
    ctx = ssl.create_default_context(cafile=args.ca)
    ctx.minimum_version = ssl.TLSVersion.TLSv1_2
    base = args.delivery.rstrip("/")

    def fetch(path: str) -> bytes:
        with urllib.request.urlopen(base + path, context=ctx, timeout=20) as res:
            body = res.read()
            print(f"CLIENT fetch path={path} status={res.status} bytes={len(body)}")
            return body

    document = json.loads(fetch(f"/v1/playback/{args.content}"))
    print(
        f"CLIENT playback grant={document.get('playback_grant')} "
        f"publication={document.get('publication')} removal={document.get('removal')} "
        f"chain_status={document.get('chain_status')} "
        f"hosting_promise={document.get('hosting_promise')}"
    )
    if document.get("playback_grant") != "AUTHORIZED" or document.get("removal") != "NONE":
        return 1
    master = fetch(f"/v1/media/{args.content}/v{document['content_version']}/master.m3u8")
    if b"#EXTM3U" not in master:
        print("CLIENT master_missing")
        return 1
    renditions = document.get("renditions") or []
    if not renditions or not renditions[0].get("segments"):
        print("CLIENT no_segment")
        return 1
    segment = renditions[0]["segments"][0]
    path = str(segment["path"])
    media = fetch(f"/v1/media/{args.content}/v{document['content_version']}/{path}")
    digest = hashlib.sha256(media).hexdigest()
    print(
        f"CLIENT segment path={path} sha256={digest} "
        f"expected={segment.get('sha256')} bytes={len(media)}"
    )
    if digest != segment.get("sha256") or len(media) != int(segment.get("byte_count")):
        return 1
    page = fetch(f"/v1/player/{args.content}")
    if b"<" not in page:
        print("CLIENT player_missing")
        return 1
    print("CLIENT ok")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"CLIENT error={type(exc).__name__}")
        raise SystemExit(1) from exc
