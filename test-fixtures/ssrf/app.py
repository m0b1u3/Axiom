"""Intentional SSRF sample for M0 scanner fixtures. Do not use in production."""

import urllib.request

from flask import Flask, request

app = Flask(__name__)


@app.get("/fetch")
def fetch():
    url = request.args.get("url", "https://example.com")
    # CWE-918: attacker-controlled URL is fetched by the server with no allowlist.
    with urllib.request.urlopen(url, timeout=5) as response:
        return {"body": response.read()[:200].decode("utf-8", errors="replace")}
