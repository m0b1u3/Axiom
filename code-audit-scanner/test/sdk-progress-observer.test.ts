import {test} from 'node:test';
import assert from 'node:assert/strict';
import {DatabaseSync} from 'node:sqlite';
import {mkdtempSync,mkdirSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {readSdkProgress} from '../src/runtime/sdk-progress-observer.js';
test('projects only the matching running scan and does not invent file completion',()=>{
 const dir=mkdtempSync(path.join(tmpdir(),'axiom-progress-'));
 try {
  assert.equal(readSdkProgress(dir),null);
  mkdirSync(path.join(dir,'runtime/sdk'),{recursive:true});
  const db=new DatabaseSync(path.join(dir,'runtime/sdk/workbench.sqlite3'));
  db.exec(`CREATE TABLE scans(id TEXT, scan_dir TEXT,status TEXT,phase TEXT,updated_at TEXT); CREATE TABLE scan_progress(scan_id TEXT,scope_file_count INTEGER,phase_items_total INTEGER,phase_items_completed INTEGER,phase_progress_unit TEXT,updated_at TEXT);`);
  db.prepare('INSERT INTO scans VALUES(?,?,?,?,?)').run('s',path.join(dir,'raw/codex-security'),'running','discovery','2026-09-10T01:30:00Z');
  db.exec("INSERT INTO scan_progress VALUES('s',175,5,2,'review_receipts','2026-09-10T01:31:00Z')");
  const p=readSdkProgress(dir)!;
  assert.equal(p.phase,'discovery'); assert.equal(p.filesTotal,175);assert.equal(p.filesTotal,175);assert.equal(p.completed,2);
  assert.equal(db.prepare('SELECT status FROM scans').get()?.status,'running');
  db.exec("UPDATE scans SET status='complete'");assert.equal(readSdkProgress(dir),null); db.close();
 } finally {rmSync(dir,{recursive:true,force:true});}
});
