"""Process queued media jobs. The shell is not used."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from svn_media.buildinfo import build_record
from svn_media.limits import Failure
from svn_media.queue import Pg, Workspace


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="svn_media")
    parser.add_argument("command", choices=("run", "claim-run", "build-record"))
    parser.add_argument("--root", type=Path)
    parser.add_argument("--pg-socket", type=Path)
    parser.add_argument("--pg-user", default="")
    parser.add_argument("--pg-database", default="")
    args = parser.parse_args(argv)
    try:
        if args.command == "build-record":
            sys.stdout.write(build_record())
            return 0
        if args.root is None:
            return _fail(Failure("REFUSED"))
        pg = None
        if args.command == "claim-run":
            if args.pg_socket is None or not args.pg_user or not args.pg_database:
                return _fail(Failure("REFUSED"))
            pg = Pg(args.pg_socket, args.pg_user, args.pg_database, _password_from_env())
            workspace = Workspace(args.root, pg=pg)
            pg.pull(workspace)
        else:
            workspace = Workspace(args.root, pg=pg)
        while True:
            record = workspace.run_once()
            if record is None:
                return 0
            sys.stdout.write(json.dumps(record, sort_keys=True) + "\n")
    except Failure as exc:
        return _fail(exc)
    except Exception:
        sys.stdout.write("INTERNAL\nProcessing failed.\n")
        return 1


def _password_from_env() -> str:
    import os

    return os.environ.get("PGPASSWORD", "")


def _fail(exc: Failure) -> int:
    sys.stdout.write(f"{exc.code}\n{exc}\n")
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
