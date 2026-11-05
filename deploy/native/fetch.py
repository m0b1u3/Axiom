#!/usr/bin/env python3
"""Fetch only hash-locked native inputs; never install or execute a download."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import tempfile
import urllib.request


def sha256(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def fetch(asset, destination):
    name = asset["filename"]
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._+-]*", name):
        raise ValueError("Unsafe asset filename")
    if not re.fullmatch(r"[a-f0-9]{64}", asset["sha256"]):
        raise ValueError("Asset must have an exact SHA-256")
    if not asset["url"].startswith("https://"):
        raise ValueError("Dependency downloads require HTTPS")
    destination = Path(destination)
    destination.mkdir(parents=True, exist_ok=True)
    target = destination / name
    if target.is_symlink():
        raise ValueError("Existing asset cannot be a link: " + name)
    if target.exists():
        if sha256(target) != asset["sha256"]:
            raise ValueError("Existing asset does not match the lock: " + name)
        return target
    request = urllib.request.Request(asset["url"], headers={"User-Agent": "Axiom-native-stage0/1"})
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(dir=destination, delete=False) as output:
            temporary = Path(output.name)
            with urllib.request.urlopen(request, timeout=60) as response:
                if not response.url.startswith("https://"):
                    raise ValueError("Dependency redirect downgraded HTTPS")
                while block := response.read(1024 * 1024):
                    output.write(block)
        if sha256(temporary) != asset["sha256"]:
            raise ValueError("Downloaded asset does not match the lock: " + name)
        # A concurrent fetch must not replace an independently created target.
        try:
            os.link(temporary, target)
        except FileExistsError:
            if target.is_symlink() or sha256(target) != asset["sha256"]:
                raise ValueError("Concurrent asset differs from the lock")
        return target
    finally:
        if temporary is not None and temporary.exists():
            temporary.unlink()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--lock", type=Path, default=Path(__file__).with_name("dependencies.lock.json"))
    parser.add_argument("--target", choices=("linux-x64", "win32-x64"), required=True)
    parser.add_argument("--destination", type=Path, required=True)
    args = parser.parse_args()
    lock = json.loads(args.lock.read_text())
    if lock.get("schemaVersion") != 1:
        raise ValueError("Unsupported native dependency lock")
    for asset in lock["assets"]:
        if args.target in asset["targets"]:
            result = fetch(asset, args.destination)
            print(json.dumps({"id": asset["id"], "file": str(result), "sha256": asset["sha256"]}), flush=True)


if __name__ == "__main__":
    main()
