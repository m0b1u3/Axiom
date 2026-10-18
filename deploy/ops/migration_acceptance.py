#!/usr/bin/env python3
"""Upgrade a frozen PostgreSQL 012 fixture; no Agent or SDK is started."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess

from acceptance import Web, wait_for
from stack import ROOT, Stack, env_bytes, private_write, read_env


def suite(env_file, work):
    work = Path(work).resolve()
    work.mkdir(parents=True, exist_ok=False)
    values = read_env(env_file)
    assert values["AXIOM_STACK_NAME"].startswith("axiom-accept-"), "Migration acceptance requires a dedicated project"
    values.update(AXIOM_STACK_NAME=values["AXIOM_STACK_NAME"]+"-migration", AXIOM_WEB_PORT="0")
    env_file = private_write(work/"runtime.env", env_bytes(values))
    stack = Stack(env_file)
    stack.ensure_unused()
    baseline = json.loads((ROOT/"deploy/release/migration-012.json").read_text())
    folder = ROOT/"code-audit-server/internal/store/migrations"
    for name, expected in baseline["files"].items():
        assert hashlib.sha256((folder/name).read_bytes()).hexdigest() == expected, "Historical migration changed: "+name
    def sql(statement):
        return stack.call("exec", "-T", "postgres", "psql", "-U", values["POSTGRES_USER"], "-d", values["POSTGRES_DB"],
                          "-At", "-v", "ON_ERROR_STOP=1", input=statement.encode(), stdout=subprocess.PIPE).stdout.decode().strip()
    try:
        stack.call("up", "-d", "--wait", "--wait-timeout", "90", "postgres", stdout=subprocess.DEVNULL)
        sql("CREATE TABLE schema_migrations(version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW());")
        for name in sorted(baseline["files"]):
            sql((folder/name).read_text()+f"\nINSERT INTO schema_migrations(version) VALUES('{name}');")
        sql("""INSERT INTO scan_tasks(scan_no,source_path,provider,model,status,attempt,max_cost_usd,created_at,finished_at)
          VALUES('fixture-pre-storage-history','/app/data/fixture-source','openai','fixture-model','completed',1,1.25,'2026-01-01T00:00:00Z','2026-01-01T00:01:00Z');
          INSERT INTO scan_attempts(task_id,attempt,agent_id,status,started_at,finished_at,result_json)
          VALUES(1,1,'fixture-old-agent','completed','2026-01-01T00:00:00Z','2026-01-01T00:01:00Z',
          '{"status":"completed","findings":[{"id":1,"scanId":1,"fingerprint":"fixture-history","title":"Fixture historical finding","severity":"low","description":"Synthetic migration evidence","raw":{"fixtureOnly":true}}]}');
          INSERT INTO findings(scan_id,fingerprint,title,severity,description,raw,created_at)
          VALUES(1,'fixture-history','Fixture historical finding','low','Synthetic migration evidence','{"fixtureOnly":true}','2026-01-01T00:01:00Z');
          INSERT INTO task_budgets(task_id,total_micros) VALUES(1,2250000);
          INSERT INTO task_cost_attempts(task_id,attempt,granted_micros,booked_micros,observed_micros,state,source,reason,updated_at)
          VALUES(1,1,1250000,250000,250000,'unknown','sdk_progress','Fixture: final cost unavailable','2026-01-01T00:01:00Z');""")
        before = sql("SELECT row_to_json(t)::text FROM scan_tasks t ORDER BY id;")
        costs = sql("SELECT row_to_json(t)::text FROM task_cost_attempts t ORDER BY task_id,attempt;")
        assert sql("SELECT to_regclass('public.storage_settings') IS NULL;") == "t"
        stack.call("up", "-d", "--no-build", "--wait", "--wait-timeout", "120", "web", stdout=subprocess.DEVNULL)
        web = Web(stack)
        web.login()
        assert sql("SELECT count(*) FROM schema_migrations;") == str(len(list(folder.glob("*.sql"))))
        assert sql("SELECT to_regclass('public.storage_settings') IS NOT NULL;") == "t"
        after = json.loads(sql("SELECT row_to_json(t)::text FROM scan_tasks t ORDER BY id;"))
        assert {name: after[name] for name in json.loads(before)} == json.loads(before), "historical task fields changed"
        assert sql("SELECT row_to_json(t)::text FROM task_cost_attempts t ORDER BY task_id,attempt;") == costs
        assert web.data("GET", "/api/v1/scans/1")["status"] == "completed"
        result = web.data("GET", "/api/v1/scans/1/attempts/1/result")
        assert result["available"] and result["findings"][0]["raw"]["fixtureOnly"] is True
        code, report, _ = web.raw("GET", "/api/v1/scans/1/report")
        assert code == 200 and b"Fixture historical finding" in report
        assert web.data("GET", "/api/v1/storage")["policy"]
        stack.call("restart", "server", stdout=subprocess.DEVNULL)
        wait_for(lambda: web.raw("GET", "/readyz")[0] == 200, 30, "upgraded Server did not become ready after restart")
        assert sql("SELECT count(*) FROM schema_migrations;") == str(len(list(folder.glob("*.sql")))), "migration replay duplicated history"
        assert "agent" not in stack.output("ps", "--status", "running", "--services").splitlines()
        report = {"fixtureOnly":True, "modelCalls":0, "agentStarted":False,
                  "baseline":"frozen-postgresql-migrations-001-through-012",
                  "target":sorted(p.name for p in folder.glob("*.sql"))[-1], "previousPublicBinaryTested":False,
                  "checks":["frozen migration hashes", "additive schema upgrade", "historical task and raw evidence",
                            "unknown cost ledger unchanged", "historical HTML report", "new Web storage policy", "idempotent restart"]}
        (work/"report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2)+"\n")
        print(json.dumps(report, ensure_ascii=False))
    finally:
        stack.call("down", "--volumes", "--remove-orphans", stdout=subprocess.DEVNULL)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env", required=True)
    parser.add_argument("--work", required=True)
    args = parser.parse_args()
    suite(args.env, args.work)
