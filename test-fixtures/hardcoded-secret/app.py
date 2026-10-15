"""Intentional hardcoded-secret sample for M0 scanner fixtures. Do not use in production."""

from flask import Flask

app = Flask(__name__)

# CWE-798: credentials committed in source. Values are fake fixtures, not real secrets.
AWS_SECRET_ACCESS_KEY = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
PAYMENT_API_SECRET = "axiom_fixture_payment_secret_never_valid"
DATABASE_PASSWORD = "SuperSecretPassword123!"


@app.get("/health")
def health():
    return {"ok": True, "db": DATABASE_PASSWORD[:3] + "..."}
