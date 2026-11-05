#!/usr/bin/env python3
"""Assemble disposable stage-0 runtimes from verified archives, not an installer."""
import argparse
import json
import os
from pathlib import Path, PurePosixPath
import platform
import shutil
import subprocess
import tarfile
import zipfile
from fetch import fetch


def unpack(archive, destination):
    destination.mkdir(parents=True, exist_ok=True)
    if archive.suffix == ".zip":
        with zipfile.ZipFile(archive) as source:
            for item in source.infolist():
                name = PurePosixPath(item.filename.replace("\\", "/"))
                if name.is_absolute() or ".." in name.parts or any(":" in part for part in name.parts):
                    raise ValueError("Unsafe dependency archive entry")
            source.extractall(destination)
    else:
        with tarfile.open(archive) as source:
            source.extractall(destination, filter="data")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--destination", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    here = Path(__file__).resolve().parent
    lock = json.loads((here / "dependencies.lock.json").read_text())
    windows = os.name == "nt"
    target = "win32-x64" if windows else "linux-x64"
    if platform.machine().lower() not in ("x86_64", "amd64"):
        raise ValueError("This dependency lock targets x64 native hosts")
    prefix = args.destination.resolve()
    prefix.mkdir(parents=True, exist_ok=False)
    args.output.mkdir(parents=True, exist_ok=True)
    components = []
    def prepare(component, operation):
        try:
            operation()
            components.append({"id": component, "status": "passed"})
        except Exception as error:
            components.append({"id": component, "status": "failed", "error": str(error)})
    def asset(id):
        value = next(a for a in lock["assets"] if a["id"] == id)
        return fetch(value, prefix / "downloads")
    def command(values, cwd, log):
        with (args.output / log).open("a", encoding="utf-8") as output:
            subprocess.run([str(v) for v in values], cwd=cwd, stdout=output, stderr=subprocess.STDOUT, check=True)
    def source_build(id, directory, configure, install, log):
        build = prefix / "build" / directory
        unpack(asset(id), build.parent)
        command(configure, build, log)
        command(["make", "-j", str(os.cpu_count() or 1)], build, log)
        command(install, build, log)
    node_dir = prefix / "node" / ("node-v24.21.0-win-x64" if windows else "node-v24.21.0-linux-x64")
    node_bin = node_dir if windows else node_dir / "bin"
    prepare("node", lambda: unpack(asset("node-" + target), prefix / "node"))
    python = prefix / "python" / ("python.exe" if windows else "bin/python3")
    git = prefix / "git" / ("cmd/git.exe" if windows else "bin/git")
    postgres = prefix / "postgres" / ("pgsql/bin" if windows else "bin")
    if windows:
        prepare("python", lambda: unpack(asset("python-windows"), prefix / "python"))
        prepare("git", lambda: unpack(asset("git-windows"), prefix / "git"))
        prepare("postgres", lambda: unpack(asset("postgres-windows"), prefix / "postgres"))
    else:
        prepare("python", lambda: source_build("python-linux-source", "Python-3.12.10",
            ["./configure", "--prefix=" + str(prefix / "python"), "--without-ensurepip", "--disable-test-modules"], ["make", "install"], "python-build.log"))
        def build_git():
            directory = prefix / "build/git-2.56.0"
            unpack(asset("git-linux-source"), directory.parent)
            command(["make", "-j", str(os.cpu_count() or 1), "prefix=" + str(prefix / "git"), "NO_TCLTK=YesPlease", "NO_GETTEXT=YesPlease", "all"], directory, "git-build.log")
            command(["make", "prefix=" + str(prefix / "git"), "NO_TCLTK=YesPlease", "NO_GETTEXT=YesPlease", "install"], directory, "git-build.log")
        prepare("git", build_git)
        prepare("postgres", lambda: source_build("postgres-linux-source", "postgresql-16.15",
            ["./configure", "--prefix=" + str(prefix / "postgres")], ["make", "install"], "postgres-build.log"))
    chrome_platform = "win64" if windows else "linux64"
    browser = prefix / "browser" / ("chrome-headless-shell-" + chrome_platform) / ("chrome-headless-shell.exe" if windows else "chrome-headless-shell")
    def browser_runtime():
        unpack(asset("chromium-" + target), prefix / "browser")
        if not windows:
            for file in browser.parent.iterdir():
                if file.is_file() and (file.suffix in ("", ".so") or file.name == "chrome-sandbox"):
                    file.chmod(file.stat().st_mode | 0o111)
    prepare("browser", browser_runtime)
    font = prefix / "fonts/NotoSansCJKsc-Regular.otf"
    def font_runtime():
        font.parent.mkdir(exist_ok=True)
        shutil.copyfile(asset("font-NotoSansCJKsc-Regular.otf"), font)
        shutil.copyfile(asset("font-LICENSE"), font.parent / "LICENSE")
    prepare("font", font_runtime)
    system_path = [os.path.join(os.environ.get("SystemRoot", "C:\\Windows"), p) for p in ("System32", "System32/Wbem", "System32/WindowsPowerShell/v1.0")] if windows else ["/usr/local/bin", "/usr/bin", "/bin"]
    paths = [str(node_bin), str(python.parent), str(git.parent), str(postgres), *system_path]
    if windows:
        paths.insert(3, str(prefix / "git/mingw64/bin"))
    result = {"schemaVersion": 1, "target": target, "root": str(prefix), "node": str(node_bin / ("node.exe" if windows else "node")),
              "npmCli": str(node_dir / ("node_modules/npm/bin/npm-cli.js" if windows else "lib/node_modules/npm/bin/npm-cli.js")),
              "python": str(python), "git": str(git), "postgresBin": str(postgres), "browser": str(browser), "font": str(font), "path": paths, "components": components}
    (args.output / "runtime.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"target": target, "components": components}), flush=True)
    if not node_bin.exists():
        raise ValueError("Node runtime could not be prepared")


if __name__ == "__main__":
    main()
