"""Intentional SQL injection sample for M0 scanner fixtures. Do not use in production."""

from flask import Flask, request

app = Flask(__name__)


@app.get("/users")
def get_user():
    user_id = request.args.get("user_id", "")
    connection = app.config["DB"]
    cursor = connection.cursor()
    # CWE-89: attacker-controlled identifier concatenated into SQL.
    cursor.execute(f"SELECT id, email FROM users WHERE id = '{user_id}'")
    return {"user": cursor.fetchone()}
