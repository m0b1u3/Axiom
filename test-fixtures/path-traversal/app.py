"""Intentional path traversal sample for M0 scanner fixtures. Do not use in production."""

import os

from flask import Flask, request

app = Flask(__name__)
BASE = "/var/data/notes"


@app.get("/notes")
def read_note():
    filename = request.args.get("file", "readme.txt")
    # CWE-22: attacker-controlled filename is joined without containment checks.
    path = os.path.join(BASE, filename)
    with open(path, encoding="utf-8") as handle:
        return {"content": handle.read()}
