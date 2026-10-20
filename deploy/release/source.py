#!/usr/bin/env python3
"""Export an auditable source candidate, never a running installation or backup."""
import argparse
import gzip
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import stat
import tarfile

ROOT = Path(__file__).resolve().parents[2]
ROOT_FILES = {".dockerignore", ".gitignore", ".gitattributes", "AGENTS.md", "README.md", "LICENSE", "NOTICE",
              "CONTRIBUTING.md", "SECURITY.md", "CHANGELOG.md", "VERSION", "DESIGN.md",
              "FRONTEND_DELIVERY.md", "PRODUCT.md", "api.md"}
SOURCE_DIRS = {".github", "code-audit-agent", "code-audit-server", "code-audit-scanner",
               "code-audit-web", "contracts", "shared", "deploy", "docs", "design", "test-fixtures"}
OMIT_DIRS = {".git", ".agents", ".codex", ".sdk-identities", ".playwright-cli", "node_modules", ".dist", "m0-out", "output",
             "__pycache__", "bin", "backups"}
OMIT_SUFFIXES = {".env", ".db", ".sqlite", ".sqlite3", ".dump", ".log", ".pem", ".key", ".p12", ".pfx", ".pyc", ".tsbuildinfo"}
MANIFEST = "SOURCE_MANIFEST.json"


def version(root):
    value = (Path(root)/"VERSION").read_text().strip()
    if not re.fullmatch(r"0\.[0-9]+\.[0-9]+-beta\.[1-9][0-9]*", value):
        raise ValueError("VERSION must identify a 0.x beta candidate")
    return value


def excluded(relative):
    parts = relative.parts
    if any(part in OMIT_DIRS for part in parts):
        return True
    if "data" in parts and parts[:2] != ("contracts", "data"):
        return True
    # Active and explicitly retained SDK replacement dist files are reviewed
    # source. Other dist directories remain generated output.
    sdk_override = (parts[:3] == ("deploy", "sdk", "overrides") or
                    parts[:5] == ("deploy", "sdk", "legacy", "0.1.25", "overrides"))
    if "dist" in parts and not sdk_override:
        return True
    name = relative.name
    if name in {".env.example", ".env.production.example"}:
        return False
    return (name == ".env" or name.startswith(".env.") or relative.suffix in OMIT_SUFFIXES
            or name.endswith((".db-shm", ".db-wal", ".sqlite-shm", ".sqlite-wal", ".sqlite3-shm", ".sqlite3-wal"))
            or name in {"database.dump", "data.tar.gz", "auth.json", ".npmrc", ".netrc", "id_rsa", "id_ed25519"})


def selected_files(root):
    root = Path(root).resolve()
    result = []
    for name in sorted(ROOT_FILES | SOURCE_DIRS):
        entry = root/name
        if not entry.exists() and not entry.is_symlink():
            continue
        pending = [entry]
        while pending:
            path = pending.pop()
            relative = path.relative_to(root)
            if excluded(relative):
                continue
            mode = path.lstat().st_mode
            if stat.S_ISLNK(mode) or not (stat.S_ISREG(mode) or stat.S_ISDIR(mode)):
                raise ValueError(f"Source contains a link or special file: {relative}")
            if stat.S_ISDIR(mode):
                pending.extend(sorted(path.iterdir(), reverse=True))
            else:
                result.append(path)
    return sorted(result)


def entry_for(path, root):
    content = path.read_bytes()
    return {"path": path.relative_to(root).as_posix(), "sizeBytes": len(content),
            "sha256": hashlib.sha256(content).hexdigest(),
            "mode": "0755" if path.stat().st_mode & 0o111 else "0644"}


def tree_hash(entries):
    return hashlib.sha256(json.dumps(entries, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def export(root, output, archive=None, revision=None):
    root, output = Path(root).resolve(), Path(output).absolute()
    if output.exists() or output.is_symlink():
        raise FileExistsError("Export destination already exists")
    output = output.resolve()
    if output == root or root in output.parents:
        raise ValueError("Export destination must be outside the source tree")
    if revision is not None and not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise ValueError("Revision must be a full Git commit SHA")
    archive = Path(archive).absolute() if archive else None
    if archive and (archive.exists() or archive.is_symlink()):
        raise FileExistsError("Archive destination already exists")
    if archive:
        archive = archive.resolve()
        if root in archive.parents or output in archive.parents or archive == output:
            raise ValueError("Archive destination must be outside the source and export trees")
        if archive.with_suffix(archive.suffix+".sha256").exists():
            raise FileExistsError("Archive checksum destination already exists")
    files = selected_files(root)
    entries = [entry_for(path, root) for path in files]
    release = version(root)
    manifest = {"schemaVersion": 1, "version": release, "sourceRevision": revision,
                "sourceTreeSha256": tree_hash(entries), "files": entries}
    output.mkdir(parents=True, mode=0o700)
    try:
        for path, record in zip(files, entries):
            destination = output/record["path"]
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(path, destination)
            destination.chmod(int(record["mode"], 8))
        (output/MANIFEST).write_text(json.dumps(manifest, ensure_ascii=False, indent=2)+"\n")
        verify(output)
        if archive:
            archive.parent.mkdir(parents=True, exist_ok=True)
            with archive.open("xb") as raw, gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as compressed:
                with tarfile.open(fileobj=compressed, mode="w", format=tarfile.PAX_FORMAT) as tar:
                    for relative in sorted([record["path"] for record in entries]+[MANIFEST]):
                        path = output/relative
                        item = tarfile.TarInfo(f"Axiom-{release}/{relative}")
                        data = path.read_bytes()
                        item.size = len(data)
                        item.mode = path.stat().st_mode & 0o777
                        tar.addfile(item, io.BytesIO(data))
            archive.with_suffix(archive.suffix+".sha256").write_text(
                hashlib.sha256(archive.read_bytes()).hexdigest()+"  "+archive.name+"\n")
    except Exception:
        shutil.rmtree(output)
        if archive and archive.is_file():
            archive.unlink()
        raise
    return manifest


def verify(directory):
    root = Path(directory).resolve()
    if (root/MANIFEST).is_symlink():
        raise ValueError("Source manifest must be a regular file")
    manifest = json.loads((root/MANIFEST).read_text())
    entries = manifest.get("files")
    if manifest.get("schemaVersion") != 1 or not isinstance(entries, list) or not entries:
        raise ValueError("Invalid source manifest")
    expected = set()
    actual = set()
    for path in root.rglob("*"):
        if path.is_symlink() or not (path.is_file() or path.is_dir()):
            raise ValueError("Source export contains a link or special file")
        if path.is_file() and path != root/MANIFEST:
            actual.add(path.relative_to(root).as_posix())
    for record in entries:
        relative = PurePosixPath(record["path"])
        if (relative.is_absolute() or ".." in relative.parts or relative.as_posix() != record["path"]
                or not relative.parts or record["path"] in expected or record["path"] == MANIFEST):
            raise ValueError("Unsafe or duplicate source manifest path")
        expected.add(record["path"])
        path = root/relative
        if not path.is_file() or entry_for(path, root) != record:
            raise ValueError(f"Source checksum or mode mismatch: {relative}")
    if expected != actual or tree_hash(entries) != manifest.get("sourceTreeSha256"):
        raise ValueError("Source tree differs from manifest")
    if version(root) != manifest.get("version"):
        raise ValueError("Source version differs from manifest")
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="action", required=True)
    make = sub.add_parser("export")
    make.add_argument("--output", required=True)
    make.add_argument("--archive")
    make.add_argument("--revision")
    inspect = sub.add_parser("verify")
    inspect.add_argument("directory")
    args = parser.parse_args()
    result = export(ROOT, args.output, args.archive, args.revision) if args.action == "export" else verify(args.directory)
    print(json.dumps({key: result[key] for key in ("version", "sourceRevision", "sourceTreeSha256")}, ensure_ascii=False))


if __name__ == "__main__":
    main()
