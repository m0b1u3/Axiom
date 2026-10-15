#!/usr/bin/env python3
"""Local administrator maintenance for the production Compose stack.

Scan configuration and execution stay in Web. This utility never calls a model.
Backup directories contain credentials and must stay private.
"""
import argparse
import base64
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import secrets
import shutil
import subprocess
import sys
import tarfile
import tempfile
from urllib.parse import urlparse, unquote

ROOT = Path(__file__).resolve().parents[2]
COMPOSE = ROOT / "deploy/docker/compose.production.yml"
APP_SERVICES = ["server", "agent", "web"]


def private_write(path, data):
    path = Path(path).resolve()
    path.parent.mkdir(parents=True, exist_ok=True)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "wb") as out:
        out.write(data)
    return path


def env_bytes(values):
    # Generated values need no dotenv interpolation or shell evaluation.
    return "".join(f"{key}={value}\n" for key, value in values.items()).encode()


def read_env(path):
    path = Path(path).resolve()
    if os.name != "nt" and path.stat().st_mode & 0o077:
        raise ValueError("Environment file must have mode 0600")
    values = {}
    for line in path.read_text().splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        key, separator, value = line.partition("=")
        if not separator or not re.fullmatch(r"[A-Z][A-Z0-9_]*", key):
            raise ValueError("Invalid environment file entry")
        if value.startswith("'") and value.endswith("'"):
            value = value[1:-1]
        elif "$" in value or value.startswith('"'):
            raise ValueError(f"Use a literal, single-quoted value for {key}")
        if "\x00" in value:
            raise ValueError(f"Invalid value for {key}")
        values[key] = value
    return values


def validate_project(value):
    if not re.fullmatch(r"[a-z0-9][a-z0-9_-]{1,62}", value):
        raise ValueError("Project name must contain 2–63 lowercase letters, digits, '-' or '_'")
    return value


def init_env(path, project, port, release="local"):
    if not re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}", release):
        raise ValueError("Release must be an explicit, valid image tag")
    if not 0 <= port <= 65535:
        raise ValueError("Port must be between 0 and 65535")
    password = secrets.token_hex(24)
    values = {
        "AXIOM_STACK_NAME": validate_project(project), "AXIOM_RELEASE": release,
        "AXIOM_IMAGE_PREFIX": "axiom", "AXIOM_BIND_ADDR": "127.0.0.1", "AXIOM_WEB_PORT": str(port),
        "POSTGRES_DB": "axiom", "POSTGRES_USER": "axiom", "POSTGRES_PASSWORD": password,
        "AXIOM_DATABASE_URL": f"postgres://axiom:{password}@postgres:5432/axiom?sslmode=disable",
        "AXIOM_ADMIN_USER": "admin", "AXIOM_ADMIN_PASSWORD": secrets.token_urlsafe(24),
        "AXIOM_JWT_SECRET": secrets.token_hex(32), "AXIOM_AGENT_TOKEN": secrets.token_hex(32),
        "AXIOM_MASTER_KEY": base64.urlsafe_b64encode(secrets.token_bytes(32)).decode().rstrip("="),
        "AXIOM_AGENT_ID": "agent-local", "AXIOM_AGENT_CONCURRENCY": "2",
        "AXIOM_MAX_ACTIVE_SCANS": "2", "AXIOM_MAX_MODEL_SCANS": "2", "AXIOM_MAX_PROJECT_SCANS": "2",
        "AXIOM_MAX_RECOVERIES": "0",
        "AXIOM_SCAN_MOCK": "0",
    }
    return private_write(path, env_bytes(values))


def retarget_env(source, destination, release, image_prefix=None):
    """Prepare an exclusive new config; preserve all credentials and data identity."""
    if not re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}", release) or release == "latest":
        raise ValueError("Release must be an explicit, valid image tag other than latest")
    if image_prefix is not None and not re.fullmatch(r"[a-z0-9][a-z0-9./:_-]{0,250}", image_prefix):
        raise ValueError("Invalid image prefix")
    read_env(source)
    updates = {"AXIOM_RELEASE": release}
    if image_prefix is not None:
        updates["AXIOM_IMAGE_PREFIX"] = image_prefix
    # Preserve original dotenv quoting, especially literal '$' in secrets.
    lines, seen = [], set()
    for line in Path(source).read_text().splitlines():
        key = line.partition("=")[0]
        if key in updates:
            line = key+"="+updates[key]
            seen.add(key)
        lines.append(line)
    lines.extend(key+"="+value for key, value in updates.items() if key not in seen)
    return private_write(destination, ("\n".join(lines)+"\n").encode())


class Stack:
    def __init__(self, env_file, extra=()):
        self.env_file = Path(env_file).resolve()
        self.values = read_env(self.env_file)
        connection = urlparse(self.values.get("AXIOM_DATABASE_URL", ""))
        if (connection.scheme not in ("postgres", "postgresql") or connection.hostname != "postgres"
                or connection.port not in (None, 5432)
                or unquote(connection.path.lstrip("/")) != self.values.get("POSTGRES_DB")
                or unquote(connection.username or "") != self.values.get("POSTGRES_USER")
                or unquote(connection.password or "") != self.values.get("POSTGRES_PASSWORD")):
            raise ValueError("Maintenance requires the PostgreSQL service and matching POSTGRES_* connection values")
        self.project = validate_project(self.values["AXIOM_STACK_NAME"])
        self.prefix = ["docker", "compose", "--env-file", str(self.env_file), "-p", self.project, "-f", str(COMPOSE)]
        for file in extra:
            self.prefix += ["-f", str(Path(file).resolve())]
        # Shell variables must not silently select another stack or secret.
        self.process_env = {k: v for k, v in os.environ.items() if not k.startswith(("AXIOM_", "POSTGRES_", "COMPOSE_"))}

    def call(self, *args, **kwargs):
        return subprocess.run(self.prefix + list(args), env=self.process_env, check=True, **kwargs)

    def output(self, *args):
        return self.call(*args, stdout=subprocess.PIPE).stdout.decode().strip()

    def ensure_unused(self):
        if self.output("ps", "--all", "--quiet"):
            raise ValueError("Restore target already has containers; choose a new project")
        volumes = subprocess.check_output(["docker", "volume", "ls", "--filter", f"label=com.docker.compose.project={self.project}", "--format", "{{.Name}}"], env=self.process_env)
        if volumes.strip():
            raise ValueError("Restore target already has volumes; choose a new project")
        # Also reject same-name volumes whose Compose labels were lost.
        for name in ("axiom-data", "axiom-postgres"):
            found = subprocess.run(["docker", "volume", "inspect", f"{self.project}_{name}"], env=self.process_env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            if found.returncode == 0:
                raise ValueError("Restore target volume exists; choose a new project")

    def image_inventory(self):
        result = {}
        for service in APP_SERVICES:
            container = self.output("ps", "--all", "--quiet", service)
            if not container:
                raise ValueError(f"Backup requires an existing {service} container")
            info = json.loads(subprocess.check_output(["docker", "inspect", container], env=self.process_env))[0]
            result[service] = {"imageId": info["Image"], "reference": info["Config"]["Image"]}
        return result


def digest(path):
    value = hashlib.sha256()
    with Path(path).open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def validate_archive(path):
    # Never restore devices, links or paths escaping /app/data. Source snapshots
    # are already link-free; reject nonportable SDK state explicitly.
    with tarfile.open(path, "r|gz") as archive:
        for member in archive:
            name = PurePosixPath(member.name)
            if name.is_absolute() or ".." in name.parts or not (member.isfile() or member.isdir()):
                raise ValueError("Data archive contains a link, device or unsafe path")


def verify_backup(directory):
    directory = Path(directory).resolve()
    manifest = json.loads((directory / "manifest.json").read_text())
    if not isinstance(manifest, dict) or manifest.get("schemaVersion") != 1:
        raise ValueError("Unsupported backup manifest version")
    expected = {"database.dump", "data.tar.gz", "runtime.env"}
    if set(manifest.get("files", {})) != expected:
        raise ValueError("Incomplete backup file inventory")
    images = manifest.get("images")
    if not isinstance(images, dict) or set(images) != set(APP_SERVICES):
        raise ValueError("Incomplete backup image inventory")
    for image in images.values():
        if not isinstance(image, dict) or not isinstance(image.get("imageId"), str) or not re.fullmatch(r"sha256:[a-f0-9]{64}", image["imageId"]) or not isinstance(image.get("reference"), str):
            raise ValueError("Invalid backup image identity")
    for name in expected:
        path = directory / name
        metadata = manifest["files"][name]
        if not isinstance(metadata, dict) or type(metadata.get("sizeBytes")) is not int or metadata["sizeBytes"] < 0 or not isinstance(metadata.get("sha256"), str) or not re.fullmatch(r"[a-f0-9]{64}", metadata["sha256"]):
            raise ValueError(f"Invalid backup metadata: {name}")
        if path.is_symlink() or path.stat().st_size != metadata["sizeBytes"] or digest(path) != metadata["sha256"]:
            raise ValueError(f"Backup checksum mismatch: {name}")
    read_env(directory / "runtime.env")
    validate_archive(directory / "data.tar.gz")
    return manifest


def backup(stack, destination):
    destination = Path(destination).resolve()
    if destination.exists():
        raise ValueError("Backup destination exists; refusing to replace it")
    destination.parent.mkdir(parents=True, exist_ok=True)
    stage = Path(tempfile.mkdtemp(prefix=".axiom-backup-", dir=destination.parent))
    stage.chmod(0o700)
    stopped = []
    try:
        running = set(stack.output("ps", "--status", "running", "--services").splitlines())
        # Close Web writes, then drain Server. Keep the Agent alive until the DB
        # proves there are no queued or active scans, so backup cannot kill one.
        for service in ("web", "server"):
            if service in running:
                stopped.append(service)
                stack.call("stop", service, stdout=subprocess.DEVNULL)
        active = stack.output("exec", "-T", "postgres", "psql", "-U", stack.values["POSTGRES_USER"], "-d", stack.values["POSTGRES_DB"], "-At", "-c",
                              "SELECT count(*) FROM scan_tasks WHERE status IN ('pending','claimed','scanning','cancel_requested')")
        if active != "0":
            raise ValueError("Backup requires an idle queue; finish or cancel scans in Web first")
        if "agent" in running:
            stopped.append("agent")
            stack.call("stop", "agent", stdout=subprocess.DEVNULL)
        with (stage / "database.dump").open("xb") as out:
            os.chmod(out.name, 0o600)
            stack.call("exec", "-T", "postgres", "pg_dump", "-U", stack.values["POSTGRES_USER"], "-d", stack.values["POSTGRES_DB"], "-Fc", stdout=out)
        with (stage / "data.tar.gz").open("xb") as out:
            os.chmod(out.name, 0o600)
            stack.call("run", "--rm", "--no-deps", "-T", "--entrypoint", "/bin/tar", "server", "-C", "/app/data", "-czf", "-", ".", stdout=out)
        private_write(stage / "runtime.env", stack.env_file.read_bytes())
        validate_archive(stage / "data.tar.gz")
        manifest = {"schemaVersion": 1, "createdAt": datetime.now(timezone.utc).isoformat(), "project": stack.project,
                    "images": stack.image_inventory(),
                    "files": {name: {"sizeBytes": (stage/name).stat().st_size, "sha256": digest(stage/name)} for name in ("database.dump", "data.tar.gz", "runtime.env")}}
        private_write(stage / "manifest.json", json.dumps(manifest, ensure_ascii=False, indent=2).encode())
        verify_backup(stage)
        stage.rename(destination)
    finally:
        shutil.rmtree(stage, ignore_errors=True)
        if stopped:
            stack.call("start", "--wait", "--wait-timeout", "90", *stopped, stdout=subprocess.DEVNULL)
    return destination


def restore(directory, env_file, project, port, extra, cleanup_on_failure=False):
    directory = Path(directory).resolve()
    manifest = verify_backup(directory)
    project = validate_project(project)
    if project == manifest["project"]:
        raise ValueError("Restore requires a different, unused project")
    values = read_env(directory / "runtime.env")
    values.update(AXIOM_STACK_NAME=project, AXIOM_WEB_PORT=str(port))
    # Exclusive creation protects an operator's existing encryption key/config.
    private_write(env_file, env_bytes(values))
    stack = Stack(env_file, extra)
    stack.ensure_unused()
    for service in APP_SERVICES:
        image = manifest["images"][service]
        actual = subprocess.check_output(["docker", "image", "inspect", image["reference"], "--format", "{{.Id}}"], env=stack.process_env).decode().strip()
        if actual != image["imageId"]:
            raise ValueError(f"Backup {service} image has changed; load the original release image first")
    try:
        stack.call("config", "--quiet")
        stack.call("up", "-d", "--wait", "--wait-timeout", "90", "postgres")
        with (directory / "database.dump").open("rb") as source:
            stack.call("exec", "-T", "postgres", "pg_restore", "--exit-on-error", "--no-owner", "--no-privileges", "-U", values["POSTGRES_USER"], "-d", values["POSTGRES_DB"], stdin=source, stdout=subprocess.DEVNULL)
        # Restored accounts/passwords remain; old browser sessions must not
        # become usable in the new environment. Old pre-session backups work.
        revoke_sql = '''DO $$ BEGIN
          IF to_regclass('public.auth_sessions') IS NOT NULL THEN
            UPDATE users SET auth_version=auth_version+1;
            UPDATE auth_sessions SET revoked_at=to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') WHERE revoked_at='';
            INSERT INTO operation_audits(id,actor_name,action,resource,outcome,occurred_at,completed_at)
            VALUES(gen_random_uuid()::text,'local-restore','session.restore-revoke','session','succeeded',
              to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
              to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'));
          END IF;
        END $$;'''
        stack.call("exec", "-T", "postgres", "psql", "--set", "ON_ERROR_STOP=1", "-U", values["POSTGRES_USER"], "-d", values["POSTGRES_DB"], "-c", revoke_sql, stdout=subprocess.DEVNULL)
        with (directory / "data.tar.gz").open("rb") as source:
            stack.call("run", "--rm", "--no-deps", "-T", "--entrypoint", "/bin/tar", "server", "-C", "/app/data", "-xzf", "-", stdin=source, stdout=subprocess.DEVNULL)
        stack.call("up", "-d", "--no-build", "--wait", "--wait-timeout", "120")
    except Exception:
        # Only the disposable acceptance harness opts into cleanup, after the
        # unused-project guard. Administrator restores remain for diagnosis.
        if cleanup_on_failure:
            stack.call("down", "--volumes", "--remove-orphans", stdout=subprocess.DEVNULL)
        raise
    return stack


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="action", required=True)
    init = commands.add_parser("init", help="create private production configuration without displaying secrets")
    init.add_argument("env_file")
    init.add_argument("--project", default="axiom-prod")
    init.add_argument("--port", type=int, default=8088)
    init.add_argument("--release", default="local", help="explicit immutable image tag; defaults to local for existing development flows")
    target = commands.add_parser("retarget", help="prepare a new image-selection configuration without changing containers or data")
    target.add_argument("source")
    target.add_argument("destination")
    target.add_argument("--release", required=True)
    target.add_argument("--image-prefix")
    save = commands.add_parser("backup", help="back up idle database, artifacts and required keys as one consistent set")
    save.add_argument("destination")
    save.add_argument("--env", required=True)
    save.add_argument("--compose-extra", action="append", default=[])
    load = commands.add_parser("restore", help="restore a verified backup into an unused project and new environment file")
    load.add_argument("directory")
    load.add_argument("--env", required=True)
    load.add_argument("--project", required=True)
    load.add_argument("--port", type=int, default=8089)
    load.add_argument("--compose-extra", action="append", default=[])
    verify = commands.add_parser("verify", help="verify file hashes and archive safety without Docker")
    verify.add_argument("directory")
    args = parser.parse_args()
    if args.action == "init":
        print(f"Private configuration created: {init_env(args.env_file, args.project, args.port, args.release)}")
    elif args.action == "retarget":
        print(f"Private candidate configuration created: {retarget_env(args.source, args.destination, args.release, args.image_prefix)}")
    elif args.action == "backup":
        print(f"Consistent backup created: {backup(Stack(args.env, args.compose_extra), args.destination)}")
    elif args.action == "restore":
        stack = restore(args.directory, args.env, args.project, args.port, args.compose_extra)
        print(f"Restored into isolated project: {stack.project}")
    else:
        verify_backup(args.directory)
        print("Backup file hashes and data archive verified")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, KeyError, subprocess.CalledProcessError, tarfile.TarError) as error:
        print(f"Maintenance failed: {error}", file=sys.stderr)
        sys.exit(1)
