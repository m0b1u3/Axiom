#!/usr/bin/env python3
"""Retain previous immutable Web assets without replacing the candidate entry page."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import uuid


def digest(path):
    result = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            result.update(chunk)
    return result.hexdigest()


def inventory(directory):
    directory = Path(directory)
    if directory.is_symlink() or not directory.is_dir():
        raise ValueError("Assets must be a regular directory")
    result = {}
    for path in sorted(directory.rglob("*")):
        if path.is_symlink() or not (path.is_dir() or path.is_file()):
            raise ValueError("Asset links and special files are not supported")
        if path.is_file():
            result[path.relative_to(directory).as_posix()] = digest(path)
    return result


def retain_assets(previous, candidate):
    previous, candidate = Path(previous), Path(candidate)
    if previous.is_symlink() or candidate.is_symlink():
        raise ValueError("Asset links and special files are not supported")
    previous, candidate = previous.resolve(), candidate.resolve()
    if previous == candidate or previous in candidate.parents or candidate in previous.parents:
        raise ValueError("Previous and candidate assets must be independent directories")
    old, current = inventory(previous), inventory(candidate)
    # Refuse the entire plan before copying if an immutable name changed meaning.
    collisions = [name for name in old.keys() & current.keys() if old[name] != current[name]]
    if collisions:
        raise ValueError("Immutable asset name collision: " + ", ".join(sorted(collisions)))
    retained = {name: sha for name, sha in old.items() if name not in current}
    for name, sha in retained.items():
        source, target = previous/name, candidate/name
        if digest(source) != sha:
            raise ValueError("Previous asset changed while preparing the release")
        target.parent.mkdir(parents=True, exist_ok=True)
        with source.open("rb") as reader, target.open("xb") as writer:
            shutil.copyfileobj(reader, writer)
        target.chmod(0o644)
    if inventory(candidate) != {**current, **retained}:
        raise ValueError("Merged asset inventory differs from the approved plan")
    return {"candidateAssets": len(current), "retainedAssets": len(retained),
            "mergedAssets": len(current) + len(retained), "retained": retained}


def run(args):
    return subprocess.run(args, check=True, capture_output=True).stdout


def image_assets(image, destination):
    name = "axiom-web-assets-" + uuid.uuid4().hex[:12]
    destination.mkdir()
    try:
        run(["docker", "create", "--name", name, "--network", "none", "--entrypoint", "/bin/true", image])
        run(["docker", "cp", name + ":/usr/share/nginx/html/assets/.", str(destination)])
    finally:
        subprocess.run(["docker", "rm", "-f", "--volumes", name], capture_output=True)


def build(previous_image, candidate_image, output_image, work):
    if any(not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._:/@+-]*", image)
           for image in (previous_image, candidate_image, output_image)):
        raise ValueError("Use an explicit image reference without options or whitespace")
    work = Path(work).absolute()
    if work.exists() or work.is_symlink():
        raise FileExistsError("Asset compatibility work directory must be new")
    if subprocess.run(["docker", "image", "inspect", output_image], capture_output=True).returncode == 0:
        raise ValueError("Output image tag already exists; use a unique release tag")
    # Inspect first so a missing image cannot cause an implicit network pull.
    images = {name: json.loads(run(["docker", "image", "inspect", image]))[0]
              for name, image in (("previous", previous_image), ("candidate", candidate_image))}
    work.mkdir(parents=True, mode=0o700)
    image_assets(images["previous"]["Id"], work/"previous-assets")
    image_assets(images["candidate"]["Id"], work/"assets")
    result = retain_assets(work/"previous-assets", work/"assets")
    if json.loads(run(["docker", "image", "inspect", candidate_image]))[0]["Id"] != images["candidate"]["Id"]:
        raise ValueError("Candidate tag changed during asset preparation")
    (work/"Dockerfile").write_text("FROM " + candidate_image + "\nCOPY --chown=101:101 assets/ /usr/share/nginx/html/assets/\n")
    with (work/"build.log").open("wb") as log:
        subprocess.run(["docker", "build", "--pull=false", "--network=none", "-t", output_image, str(work)],
                       check=True, stdout=log, stderr=subprocess.STDOUT)
    common = ["docker", "run", "--rm", "--pull", "never", "--network", "none", "--read-only",
              "--cap-drop", "ALL", "--security-opt", "no-new-privileges=true"]
    rows = run(common + ["--entrypoint", "find", output_image, "/usr/share/nginx/html/assets", "-type", "f",
                         "-exec", "sha256sum", "{}", ";"]).decode().splitlines()
    actual = {row.split("  ", 1)[1].removeprefix("/usr/share/nginx/html/assets/"): row.split("  ", 1)[0] for row in rows}
    if actual != inventory(work/"assets"):
        raise ValueError("Image asset content or runtime permissions differ")
    index = lambda image: run(common + ["--entrypoint", "sha256sum", image, "/usr/share/nginx/html/index.html"]).split()[0]
    if index(output_image) != index(candidate_image):
        raise ValueError("Compatibility image replaced the candidate entry page")
    result.update({"status": "passed", "previousImageId": images["previous"]["Id"],
                   "candidateImageId": images["candidate"]["Id"], "outputImage": output_image,
                   "outputImageId": json.loads(run(["docker", "image", "inspect", output_image]))[0]["Id"],
                   "indexUnchanged": True, "network": "none", "businessVolumesMounted": False, "realModelCalls": 0})
    (work/"report.json").write_text(json.dumps(result, indent=2) + "\n")
    return result


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for flag in ("previous-image", "candidate-image", "output-image", "work"):
        parser.add_argument("--" + flag, required=True)
    args = parser.parse_args()
    result = build(args.previous_image, args.candidate_image, args.output_image, args.work)
    print(json.dumps({k: v for k, v in result.items() if k != "retained"}))
