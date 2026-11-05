#!/usr/bin/env python3
"""Verify npm SRI before deriving a complete, platform-specific SDK inventory."""
import base64
import hashlib
import json
from pathlib import PurePosixPath, Path
import tarfile


def npm_inventory(archive, package, install_path, platform, integrity):
    algorithm, expected = integrity.split("-", 1)
    if algorithm != "sha512":
        raise ValueError("A pinned SHA-512 npm integrity is required")
    with Path(archive).open("rb") as stream:
        actual = base64.b64encode(hashlib.file_digest(stream, algorithm).digest()).decode()
    if actual != expected:
        raise ValueError("npm archive integrity mismatch")
    files = {}
    metadata = None
    with tarfile.open(archive, "r:gz") as source:
        for member in source:
            name = PurePosixPath(member.name)
            if name.is_absolute() or ".." in name.parts or not name.parts or name.parts[0] != "package":
                raise ValueError("Unsafe npm archive path")
            if member.isdir():
                continue
            if not member.isfile():
                raise ValueError("Unexpected npm archive link or special file")
            relative = PurePosixPath(*name.parts[1:]).as_posix()
            if relative in files:
                raise ValueError("Duplicate npm archive file")
            with source.extractfile(member) as stream:
                data = stream.read()
            files[relative] = hashlib.sha256(data).hexdigest()
            if relative == "package.json":
                metadata = json.loads(data)
    if not metadata or metadata["name"] != package:
        raise ValueError("npm package identity mismatch")
    return {"path": install_path, "name": package, "version": metadata["version"],
            "files": dict(sorted(files.items())), "platform": platform}
