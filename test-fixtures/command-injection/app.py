"""Intentional OS command injection sample for M0 scanner fixtures. Do not use in production."""

import os

from flask import Flask, request

app = Flask(__name__)


@app.get("/ping")
def ping():
    host = request.args.get("host", "127.0.0.1")
    # CWE-78: attacker-controlled host concatenated into a shell command.
    os.system("ping -c 1 " + host)
    return {"ok": True, "host": host}
