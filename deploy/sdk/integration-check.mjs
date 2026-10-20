// Read-only SDK surface inspection. Never import the SDK: importing a runtime
// must not turn a capabilities check into a scan, login or publication action.
import {readFile, stat} from 'node:fs/promises';
import {createHash} from 'node:crypto';
import {createRequire} from 'node:module';
import path from 'node:path';
import {fileURLToPath, pathToFileURL} from 'node:url';

const repositoryRoot=fileURLToPath(new URL('../../',import.meta.url));
const require=createRequire(new URL('../../code-audit-scanner/package.json',import.meta.url));
const ts=require('typescript');
const digest=text=>createHash('sha256').update(text).digest('hex');

function declarationSurface(sources) {
  const declarations=new Map(),exports=new Map();
  for(const [filename,source] of sources) {
    const ast=ts.createSourceFile(filename,source,ts.ScriptTarget.Latest,true,ts.ScriptKind.TS);
    for(const statement of ast.statements) {
      if((ts.isClassDeclaration(statement)||ts.isInterfaceDeclaration(statement)) && statement.name) declarations.set(statement.name.text,statement);
      if(ts.isExportDeclaration(statement) && !statement.isTypeOnly && statement.exportClause && ts.isNamedExports(statement.exportClause)) {
        const list=exports.get(filename)??new Set();
        for(const item of statement.exportClause.elements) if(!item.isTypeOnly) list.add(item.name.text);
        exports.set(filename,list);
      }
    }
  }
  function member(type,name,seen=new Set()) {
    if(seen.has(type))return false;seen.add(type);
    const decl=declarations.get(type);
    if(decl?.members.some(m=>m.name && m.name.getText().replaceAll('"','').replaceAll("'",'')===name))return true;
    // 0.5.0 generates ScanSettings from a Zod object; inspect its static type
    // literal, without importing or evaluating a runtime or allowing arbitrary names.
    if(type==='ScanSettings'||type==='DeepScanOptions') {
      const schema=type==='ScanSettings'?'ScanSettingsSchema':'DeepScanSettingsSchema';
      for(const [filename,source] of sources)if(filename==='dist/scan-settings.d.ts') {
        const start=source.indexOf('const '+schema+':'),end=source.indexOf('}, z.core.$strict>',start);
        if(start>=0&&end>start&&new RegExp('^    '+name+':','m').test(source.slice(start,end)))return true;
      }
    }
    if(!decl)return false;
    for(const clause of decl.heritageClauses??[])for(const base of clause.types)if(member(base.expression.getText(),name,seen))return true;
    return false;
  }
  const root=exports.get('dist/index.d.ts')??new Set(),server=exports.get('dist/server/api.d.ts')??new Set();
  function has(api) {
    const prefix='@openai/codex-security/server.';
    if(api.startsWith(prefix))return server.has(api.slice(prefix.length));
    const dot=api.indexOf('.');
    if(dot>=0)return member(api.slice(0,dot),api.slice(dot+1));
    return root.has(api);
  }
  return {has,publicExports:[...root].sort(),serverExports:[...server].sort()};
}

export async function checkSdkIntegration(options={}) {
  const root=options.repositoryRoot??repositoryRoot;
  const sdkDirectory=options.sdkDirectory??path.join(root,'code-audit-scanner/node_modules/@openai/codex-security');
  const manifest=options.manifest??JSON.parse(await readFile(path.join(root,'deploy/sdk/integration.json'),'utf8'));
  const project=JSON.parse(await readFile(path.join(root,'code-audit-scanner/package.json'),'utf8'));
  const installed=JSON.parse(await readFile(path.join(sdkDirectory,'package.json'),'utf8'));
  if(manifest.schemaVersion!==1 || manifest.sdk.package!==installed.name || manifest.sdk.version!==installed.version || project.dependencies[installed.name]!==installed.version)throw new Error('SDK integration baseline version mismatch; upgrades require separate compatibility acceptance');
  if(manifest.constraints.userSurface!=='web' || manifest.constraints.autoUpgrade!==false)throw new Error('SDK integration must retain Web operations and an explicit version pin');
  const sources=new Map();
  for(const [name,sha256] of Object.entries(manifest.sdk.declarations)) {
    if(!name.startsWith('dist/') || name.split('/').includes('..') || !name.endsWith('.d.ts'))throw new Error('Unsafe SDK declaration reference');
    const source=await readFile(path.join(sdkDirectory,name),'utf8');
    if(digest(source)!==sha256)throw new Error(`SDK declaration changed: ${name}`);
    sources.set(name,source);
  }
  const surface=declarationSurface(sources),ids=new Set(),counts={};
  for(const capability of manifest.capabilities) {
    if(ids.has(capability.id))throw new Error('Duplicate SDK capability');ids.add(capability.id);
    if(!Object.hasOwn(manifest.statusDefinitions,capability.status))throw new Error('Unknown SDK integration status');
    if(!capability.api.length || !capability.notes || !Number.isSafeInteger(capability.phase))throw new Error('Incomplete SDK capability');
    for(const api of capability.api) {
      if(capability.status==='not_public' ? surface.has(api) : !surface.has(api))throw new Error(`SDK public API mismatch: ${capability.id} / ${api}`);
    }
    if(['integrated','partial','sdk_internal_use'].includes(capability.status) && !capability.evidence.length)throw new Error(`Missing implementation evidence: ${capability.id}`);
    for(const evidence of capability.evidence) {
      const target=path.resolve(root,evidence);
      if(!target.startsWith(path.resolve(root)+path.sep) || !(await stat(target)).isFile())throw new Error(`Invalid implementation evidence: ${capability.id}`);
    }
    counts[capability.status]=(counts[capability.status]??0)+1;
  }
  const classified=new Set(manifest.capabilities.filter(c=>c.status!=='not_public').flatMap(c=>c.api.map(api=>api.split('.')[0])));
  for(const [name,reason] of Object.entries(manifest.nonWorkflowExports??{})) {
    if(!surface.publicExports.includes(name)||typeof reason!=='string'||!reason.trim())throw new Error(`Invalid non-workflow export: ${name}`);
    classified.add(name);
  }
  for(const name of surface.publicExports)if(!classified.has(name))throw new Error(`Unclassified SDK export: ${name}`);
  return {sdk:installed.name,version:installed.version,declarationFiles:sources.size,capabilityGroups:ids.size,counts,publicExports:surface.publicExports,serverExports:surface.serverExports,modelCalls:0,externalWrites:0};
}

if(process.argv[1] && import.meta.url===pathToFileURL(path.resolve(process.argv[1])).href) {
  try {console.log(JSON.stringify(await checkSdkIntegration(),null,2));}
  catch(error){console.error(error.message);process.exitCode=1;}
}
