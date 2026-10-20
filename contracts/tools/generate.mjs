import {readFile, readdir, mkdir, writeFile} from 'node:fs/promises';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {createHash} from 'node:crypto';
import {spawnSync} from 'node:child_process';
import {Ajv2020} from 'ajv/dist/2020.js';
import addFormats from 'ajv-formats';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const check = process.argv.includes('--check');
const schemaDir = path.join(root, 'schemas');
const entries = [];
async function schemaNames(directory, prefix = '') {
  const names = [];
  for (const entry of await readdir(directory, {withFileTypes:true})) {
    if (entry.isDirectory()) names.push(...await schemaNames(path.join(directory,entry.name),prefix+entry.name+'/'));
    else if (entry.name.endsWith('.json')) names.push(prefix+entry.name);
  }
  return names.sort();
}
for (const name of await schemaNames(schemaDir)) {
  const text = await readFile(path.join(schemaDir, name), 'utf8');
  entries.push({name, text, schema: JSON.parse(text)});
}
const metadataText = await readFile(path.join(root, 'data/compatibility.json'), 'utf8');
const metadata = JSON.parse(metadataText);
// New contracts may own their definitions without rewriting historical schemas.
const defs = {};
for (const entry of entries) for (const [name, definition] of Object.entries(entry.schema.$defs ?? {})) {
  if (Object.hasOwn(defs, name)) throw new Error('Duplicate contract definition: ' + name);
  defs[name] = definition;
}
const roots = entries.filter(e => e.schema.title).map(e => ({name: e.schema.title, id: e.schema.$id}));
const names = new Set(Object.keys(defs));
const allowed = new Set(['$schema','$id','$defs','$ref','title','description','type','properties','required','additionalProperties','enum','const','items','minimum','maximum','exclusiveMinimum','minLength','maxLength','pattern','format','uniqueItems','allOf','anyOf','oneOf','not','if','then','else','minItems','maxItems','maxProperties']);
function inspect(s) {
  if (typeof s !== 'object' || s === null) throw new Error('Schema nodes must be objects');
  for (const key of Object.keys(s)) if (!allowed.has(key)) throw new Error('Unsupported schema keyword: ' + key);
  if (s.$ref && !names.has(s.$ref.split('/').at(-1))) throw new Error('Unknown definition: ' + s.$ref);
  for (const v of Object.values(s.properties ?? {})) inspect(v);
  for (const key of ['allOf','anyOf','oneOf']) for (const v of s[key] ?? []) inspect(v);
  for (const key of ['items','not','if','then','else']) if (s[key]) inspect(s[key]);
  if (typeof s.additionalProperties === 'object') inspect(s.additionalProperties);
}
for (const s of Object.values(defs)) inspect(s);
for (const [from, to] of Object.entries(metadata.legacyErrorAliases)) {
  if (defs.ErrorCode.enum.includes(from) || !defs.ErrorCode.enum.includes(to)) throw new Error('Invalid error alias: ' + from);
}
if (JSON.stringify(Object.keys(metadata.exitCodes).sort()) !== JSON.stringify([...defs.TerminalStatus.enum].sort())) throw new Error('Exit statuses drifted');
if (Object.values(metadata.limits).some(n => !Number.isSafeInteger(n) || n <= 0)) throw new Error('Invalid byte limits');
if (!defs.ScanMode.enum.includes(metadata.defaults.scanMode) || !defs.ValidationMode.enum.includes(metadata.defaults.validationMode) || !Number.isSafeInteger(metadata.defaults.scanSeconds) || metadata.defaults.scanSeconds <= 0) throw new Error('Invalid default catalog');
const ajv = new Ajv2020({strict: true, strictTypes: false, strictRequired: false, allErrors: true, coerceTypes: false, useDefaults: false, removeAdditional: false});
addFormats(ajv);
for (const e of entries) ajv.addSchema(e.schema);
for (const r of roots) ajv.getSchema(r.id);
const scanInputPolicyText=await readFile(path.join(root,'data/scan-input-compatibility.json'),'utf8');
const scanInputPolicy=JSON.parse(scanInputPolicyText);
if(scanInputPolicy.schemaVersion!==1||scanInputPolicy.inputVersion!==defs.ScanInputAssignment.properties.schemaVersion.const||scanInputPolicy.jobVersion!==defs.ScannerJobV4.properties.schemaVersion.const||scanInputPolicy.descriptionVersion!==defs.ScannerDescriptionV5.properties.schemaVersion.const||scanInputPolicy.controlAPI!==4||scanInputPolicy.knowledgeMaxDocuments!==defs.ScanInput.properties.knowledge.maxItems||scanInputPolicy.knowledgeDocumentMaxBytes!==defs.KnowledgeDocument.properties.content.maxLength)throw new Error('Scan input compatibility drift');
const sdkOperationPolicyText=await readFile(path.join(root,'data/sdk-operation-compatibility.json'),'utf8');
const sdkOperationPolicy=JSON.parse(sdkOperationPolicyText);
if(sdkOperationPolicy.inputVersion!==3||sdkOperationPolicy.jobVersion!==defs.ScannerJobV5.properties.schemaVersion.const||sdkOperationPolicy.descriptionVersion!==defs.ScannerDescriptionV6.properties.schemaVersion.const||sdkOperationPolicy.operationVersion!==defs.SDKOperation.properties.schemaVersion.const||sdkOperationPolicy.resultVersion!==defs.SDKOperationResult.properties.schemaVersion.const||sdkOperationPolicy.sessionEventVersion!==defs.SDKSessionEvent.properties.schemaVersion.const||sdkOperationPolicy.sessionSummaryVersion!==defs.SDKSessionSummary.properties.schemaVersion.const||sdkOperationPolicy.controlAPI!==4)throw new Error('SDK operation compatibility drift');
const gitDiffPolicyText=await readFile(path.join(root,'data/git-diff-compatibility.json'),'utf8');
const gitDiffPolicy=JSON.parse(gitDiffPolicyText);
if(gitDiffPolicy.inputVersion!==4||gitDiffPolicy.jobVersion!==defs.ScannerJobV6.properties.schemaVersion.const||gitDiffPolicy.descriptionVersion!==defs.ScannerDescriptionV7.properties.schemaVersion.const)throw new Error('Git diff compatibility drift');
const componentPolicyText=await readFile(path.join(root,'data/component-planning-compatibility.json'),'utf8');
const componentPolicy=JSON.parse(componentPolicyText);
if(componentPolicy.inputVersion!==5||componentPolicy.jobVersion!==defs.ScannerJobV7.properties.schemaVersion.const||componentPolicy.descriptionVersion!==defs.ScannerDescriptionV8.properties.schemaVersion.const)throw new Error('Component planning compatibility drift');
const componentScanPolicyText=await readFile(path.join(root,'data/component-scan-compatibility.json'),'utf8');
const componentScanPolicy=JSON.parse(componentScanPolicyText);
if(componentScanPolicy.inputVersion!==6||componentScanPolicy.jobVersion!==defs.ScannerJobV8.properties.schemaVersion.const||componentScanPolicy.descriptionVersion!==defs.ScannerDescriptionV9.properties.schemaVersion.const)throw new Error('Component scan compatibility drift');
const sdkIdentityPolicyText=await readFile(path.join(root,'data/sdk-identity-compatibility.json'),'utf8');
const sdkIdentityPolicy=JSON.parse(sdkIdentityPolicyText);
if(sdkIdentityPolicy.identityVersion!==defs.SDKTaskIdentity.properties.schemaVersion.const||sdkIdentityPolicy.descriptionVersion!==defs.ScannerDescriptionV10.properties.schemaVersion.const||sdkIdentityPolicy.controlAPI!==4)throw new Error('SDK identity compatibility drift');
const sdkParametersPolicyText=await readFile(path.join(root,'data/sdk-parameters-compatibility.json'),'utf8');
const sdkParametersPolicy=JSON.parse(sdkParametersPolicyText);
if(sdkParametersPolicy.descriptionVersion!==defs.ScannerDescriptionV11.properties.schemaVersion.const||sdkParametersPolicy.parametersVersion!==defs.SDKScanParameters.properties.schemaVersion.const||sdkParametersPolicy.assignmentVersion!==defs.SDKParametersAssignment.properties.schemaVersion.const||sdkParametersPolicy.sdkVersion!=='0.1.25')throw new Error('SDK parameters compatibility drift');
const concurrencyPolicyText=await readFile(path.join(root,'data/sdk-concurrency-compatibility.json'),'utf8');
const concurrencyPolicy=JSON.parse(concurrencyPolicyText);
if(concurrencyPolicy.concurrencyVersion!==1||concurrencyPolicy.descriptionVersion!==defs.ScannerDescriptionV12.properties.schemaVersion.const||concurrencyPolicy.sdkVersion!=='0.1.25'||concurrencyPolicy.maxSafeInteger!==Number.MAX_SAFE_INTEGER)throw new Error('SDK concurrency compatibility drift');
for(const field of ['workers','deepMaxDiscoveryRuns','deepStopAfterNoNew'])if(defs.ScanPolicy.properties[field].maximum!==concurrencyPolicy.maxSafeInteger)throw new Error('Deep SDK integer boundary drift');
if(defs.ScanInput.properties.subagents.maximum!==concurrencyPolicy.maxSafeInteger||defs.ComponentScanRequest.properties.workers.maximum!==concurrencyPolicy.maxSafeInteger)throw new Error('SDK concurrency integer boundary drift');
const adapterPolicyText=await readFile(path.join(root,'data/sdk-adapter-compatibility.json'),'utf8');
const adapterPolicy=JSON.parse(adapterPolicyText);
if(adapterPolicy.sdkVersion!==defs.ScannerDescriptionV13.properties.configuredSdk.properties.version.const||adapterPolicy.descriptionVersion!==defs.ScannerDescriptionV13.properties.schemaVersion.const||adapterPolicy.adapterVersion!==defs.ScannerDescriptionV13.properties.sdkResultAdapterVersion.const)throw new Error('SDK result adapter baseline drift');
const hash = createHash('sha256');
hash.update('sdk-adapter-compatibility.json\0'+adapterPolicyText);
for (const e of entries) hash.update(e.name + '\0' + e.text);
hash.update('compatibility.json\0' + metadataText);
hash.update('sdk-operation-compatibility.json\0'+sdkOperationPolicyText);
hash.update('scan-input-compatibility.json\0'+scanInputPolicyText);
hash.update('git-diff-compatibility.json\0'+gitDiffPolicyText);
hash.update('component-planning-compatibility.json\0'+componentPolicyText);
hash.update('component-scan-compatibility.json\0'+componentScanPolicyText);
hash.update('sdk-identity-compatibility.json\0'+sdkIdentityPolicyText);
hash.update('sdk-parameters-compatibility.json\0'+sdkParametersPolicyText);
hash.update('sdk-concurrency-compatibility.json\0'+concurrencyPolicyText);
const digest = hash.digest('hex');
const header = '// Generated by contracts/tools/generate.mjs; DO NOT EDIT.\n// Schema SHA-256: ' + digest + '\n';
const goName = value => value.replace(/([a-z0-9])([A-Z])/g, '$1 $2').split(/[^A-Za-z0-9]+/).filter(Boolean).map(w => ({id:'ID',json:'JSON',sdk:'SDK',url:'URL',api:'API',usd:'USD',sha256:'SHA256'}[w.toLowerCase()] ?? w[0].toUpperCase() + w.slice(1))).join('');
const refName = s => s.$ref.split('/').at(-1);
function unnull(s) {
  if (s.anyOf?.length === 2 && s.anyOf.some(v => v.type === 'null')) {
    return {base:s.anyOf.find(v => v.type !== 'null'), nullable:true};
  }
  return {base:s, nullable:false};
}
function tsType(s) {
  const {base, nullable} = unnull(s);
  let result;
  if (base.$ref) result = refName(base);
  else if (base.const !== undefined) result = JSON.stringify(base.const);
  else if (base.enum) result = base.enum.map(v => JSON.stringify(v)).join(' | ');
  else if (base.type === 'string') result = 'string';
  else if (base.type === 'integer' || base.type === 'number') result = 'number';
  else if (base.type === 'boolean') result = 'boolean';
  else if (base.type === 'array') result = 'Array<' + tsType(base.items) + '>';
  else if (base.type === 'object') result = tsObject(base);
  else throw new Error('Unrepresentable TS type: ' + JSON.stringify(base));
  return nullable ? '(' + result + ') | null' : result;
}
function tsObject(s, excluded = []) {
  if (!s.properties) return 'Record<string, ' + tsType(s.additionalProperties) + '>';
  const required = new Set(s.required ?? []);
  return '{\n' + Object.entries(s.properties).filter(([n]) => !excluded.includes(n)).map(([n,v]) => '  ' + n + (required.has(n) ? '' : '?') + ': ' + tsType(v) + ';').join('\n') + '\n}';
}
let ts = header + '\nexport type JsonValue = null | boolean | string | number | JsonValue[] | {[key: string]: JsonValue};\n';
for (const name of [...names].sort()) {
  if (name === 'JsonValue') continue;
  const s = defs[name];
  const type = ['ScanEvent','ScanEventV2'].includes(name) ? tsObject(s,['type','data']) + ' & (\n' + s.oneOf.map(v => '  | {type: ' + JSON.stringify(v.properties.type.const) + '; data: ' + tsType(v.properties.data) + '}').join('\n') + '\n)' : tsType(s);
  ts += '\nexport type ' + name + ' = ' + type + ';\n';
}
ts += '\nexport interface ContractTypes {\n' + roots.map(r => '  ' + r.name + ': ' + r.name + ';').join('\n') + '\n}\nexport type ContractKind = keyof ContractTypes;\n';
ts += 'export const SCHEMA_VERSION = ' + defs.SchemaVersion.const + ' as const;\n';
ts += 'export const SCHEMA_HASH = ' + JSON.stringify(digest) + ';\n';
ts += 'export const COMPATIBILITY = ' + JSON.stringify(metadata,null,2) + ' as const;\n';
ts += 'export const SCAN_INPUT_POLICY = '+JSON.stringify(scanInputPolicy)+' as const;\n';
ts += 'export const SDK_ADAPTER_POLICY = '+JSON.stringify(adapterPolicy)+' as const;\n';
ts += 'export const SDK_PARAMETERS_POLICY = '+JSON.stringify(sdkParametersPolicy)+' as const;\n';
ts += 'export const SDK_IDENTITY_POLICY = '+JSON.stringify(sdkIdentityPolicy)+' as const;\n';
ts += 'export const SDK_OPERATION_POLICY = '+JSON.stringify(sdkOperationPolicy)+' as const;\n';
ts += 'export const COMPONENT_PLANNING_POLICY = '+JSON.stringify(componentPolicy)+' as const;\n';
ts += 'export const GIT_DIFF_POLICY = '+JSON.stringify(gitDiffPolicy)+' as const;\n';
ts += 'export const CONTRACT_KINDS = ' + JSON.stringify(roots.map(r=>r.name)) + ' as const;\n';

const extraDefs = new Map();
function goType(s, context) {
  const {base,nullable} = unnull(s);
  let result;
  if (base.$ref) result = goName(refName(base));
  else if (base.type === 'array') result = '[]' + goType(base.items, context+'Item');
  else if (base.type === 'object' && !base.properties) result = 'map[string]' + goType(base.additionalProperties,context+'Value');
  else if (base.type === 'object' || base.enum) {
    if (names.has(context) || extraDefs.has(context)) throw new Error('Generated name collision: ' + context);
    extraDefs.set(context,base); result = goName(context);
  }
  else result = {string:'string',integer:'int64',number:'float64',boolean:'bool'}[base.type];
  if (!result) throw new Error('Unrepresentable Go type: ' + JSON.stringify(base));
  return nullable && result !== 'JSONValue' ? '*' + result : result;
}
function goDef(name,s) {
  const n = goName(name);
  if (name === 'JsonValue') return 'type JSONValue = json.RawMessage\n';
  if (s.type === 'object' && s.properties) {
    const req = new Set(s.required ?? []);
    return 'type ' + n + ' struct {\n' + Object.entries(s.properties).map(([key,value]) => {
      let type = goType(value,name+goName(key));
      const optional = !req.has(key);
      if (optional) type = 'Optional[' + type + ']';
      const tag = 'json:"' + key + (optional ? ',omitzero' : '') + '"';
      return '  ' + goName(key) + ' ' + type + ' ' + String.fromCharCode(96) + tag + String.fromCharCode(96);
    }).join('\n') + '\n}\n';
  }
  if (s.enum) {
    const type = s.type === 'string' ? 'string' : 'int64';
    return 'type ' + n + ' ' + type + '\nconst (\n' + s.enum.map(value => '  ' + n + goName(String(value)) + ' ' + n + ' = ' + JSON.stringify(value)).join('\n') + '\n)\n';
  }
  if (s.type === 'object') return 'type ' + n + ' map[string]' + goType(s.additionalProperties,name+'Value') + '\n';
  const type = {string:'string',integer:'int64',number:'float64',boolean:'bool'}[s.type];
  if (!type) throw new Error('Unrepresentable definition: ' + name);
  return 'type ' + n + ' ' + type + '\n';
}
let go = header + '\npackage contracts\n\nimport "encoding/json"\n\n';
for (const name of [...names].sort()) go += goDef(name,defs[name]) + '\n';
for (const [name,s] of extraDefs) go += goDef(name,s) + '\n';
go += 'type Kind string\nconst (\n' + roots.map(r=>'  Kind'+r.name+' Kind = '+JSON.stringify(r.name)).join('\n') + '\n)\n';
go += 'const Version SchemaVersion = ' + defs.SchemaVersion.const + '\nconst SchemaHash = ' + JSON.stringify(digest) + '\n';

let codec = header + '\npackage contracts\n\nimport "encoding/json"\n\nfunc RoundTrip(kind Kind, data []byte, expected *ExecutionRef) ([]byte,error) {\n';
codec += '  if err:=ValidateJSON(kind,data,expected);err!=nil{return nil,err}\n  var value any\n  switch kind {\n';
for (const r of roots) codec += '  case Kind'+r.name+': value = new('+r.name+')\n';
codec += '  default: return nil, invalid("unsupported_kind",kind)\n  }\n  canonical,err:=normalizedJSON(data);if err!=nil{return nil,invalid("invalid_json",kind)}\n  if err:=json.Unmarshal(canonical,value);err!=nil{return nil,invalid("invalid_json",kind)}\n  result,err:=EncodeJSON(value);if err!=nil{return nil,invalid("invalid_json",kind)}\n  if err:=ValidateJSON(kind,result,expected);err!=nil{return nil,err}\n  return result,nil\n}\n';
for (const r of roots) codec += '\nfunc Decode'+r.name+'(data []byte,expected *ExecutionRef) ('+r.name+',error) {\n  var v '+r.name+'\n  if err:=ValidateJSON(Kind'+r.name+',data,expected);err!=nil{return v,err}\n  canonical,err:=normalizedJSON(data);if err!=nil{return v,invalid("invalid_json",Kind'+r.name+')}\n  if err:=json.Unmarshal(canonical,&v);err!=nil{return v,invalid("invalid_json",Kind'+r.name+')}\n  return v,nil\n}\n';
for (const eventName of ['ScanEvent','ScanEventV2']) for (const variant of defs[eventName].oneOf) {
  const kind = variant.properties.type.const;
  const type = refName(variant.properties.data);
  codec += '\nfunc (event '+eventName+') As'+type+'() ('+type+',error) {\n  var value '+type+'\n  data,err:=EncodeJSON(event);if err!=nil{return value,invalid("invalid_json",Kind'+eventName+')}\n  if err:=ValidateJSON(Kind'+eventName+',data,nil);err!=nil{return value,err}\n  if event.Type!='+JSON.stringify(kind)+'{return value,invalid("wrong_event_type",Kind'+eventName+')}\n  canonical,err:=normalizedJSON(event.Data);if err!=nil{return value,invalid("invalid_json",Kind'+eventName+')}\n  if err:=json.Unmarshal(canonical,&value);err!=nil{return value,invalid("invalid_json",Kind'+eventName+')}\n  return value,nil\n}\n';
}
let resources = header + '\npackage contracts\n\nvar schemaResources = map[string]string{\n' + entries.map(e=>'  '+JSON.stringify(e.schema.$id)+': '+JSON.stringify(JSON.stringify(e.schema))+',').join('\n') + '\n}\nvar schemaIDs = map[Kind]string{\n' + roots.map(r=>'  Kind'+r.name+': '+JSON.stringify(r.id)+',').join('\n') + '\n}\n';
resources += 'var legacyErrorAliases=map[string]ErrorCode{\n'+Object.entries(metadata.legacyErrorAliases).map(([k,v])=>JSON.stringify(k)+': '+JSON.stringify(v)+',').join('\n')+'\n}\n';
resources += 'var terminalExitCodes=map[TerminalStatus]int{\n'+Object.entries(metadata.exitCodes).map(([k,v])=>JSON.stringify(k)+': '+v+',').join('\n')+'\n}\n';
resources += 'var canonicalErrorCodes=map[ErrorCode]bool{\n'+defs.ErrorCode.enum.map(k=>JSON.stringify(k)+': true,').join('\n')+'\n}\n';
resources += 'const MaxDocumentBytes='+metadata.limits.documentBytes+'\nconst MaxProgressBytes='+metadata.limits.progressBytes+'\nconst MaxEventBytes='+metadata.limits.eventBytes+'\nconst DeepFinalizationGraceSeconds='+metadata.deepFinalizationGraceSeconds+'\n';
resources += 'const DefaultScanMode ScanMode='+JSON.stringify(metadata.defaults.scanMode)+'\nconst DefaultScanSeconds='+metadata.defaults.scanSeconds+'\nconst DefaultValidationMode ValidationMode='+JSON.stringify(metadata.defaults.validationMode)+'\n';
resources += 'const ScanInputVersion='+scanInputPolicy.inputVersion+'\n';
resources += 'const ScanInputJobVersion='+scanInputPolicy.jobVersion+'\nconst ScanInputDescriptionVersion='+scanInputPolicy.descriptionVersion+'\nconst KnowledgeMaxDocuments='+scanInputPolicy.knowledgeMaxDocuments+'\nconst KnowledgeDocumentMaxBytes='+scanInputPolicy.knowledgeDocumentMaxBytes+'\n';
resources += 'const SDKOperationInputVersion='+sdkOperationPolicy.inputVersion+'\nconst SDKOperationJobVersion='+sdkOperationPolicy.jobVersion+'\nconst SDKOperationDescriptionVersion='+sdkOperationPolicy.descriptionVersion+'\n';
resources += 'const GitDiffInputVersion='+gitDiffPolicy.inputVersion+'\nconst GitDiffJobVersion='+gitDiffPolicy.jobVersion+'\nconst GitDiffDescriptionVersion='+gitDiffPolicy.descriptionVersion+'\n';
resources += 'const ComponentPlanningInputVersion='+componentPolicy.inputVersion+'\nconst ComponentPlanningJobVersion='+componentPolicy.jobVersion+'\nconst ComponentPlanningDescriptionVersion='+componentPolicy.descriptionVersion+'\n';
resources += 'const SDKParametersVersion=1\nconst SDKParametersDescriptionVersion=11\n';
resources += 'const SDKConcurrencyVersion=1\nconst SDKConcurrencyDescriptionVersion=12\nconst SDKMaxSafeInteger=9007199254740991\n';
resources += Object.entries(concurrencyPolicy.legacy).map(([k,v])=>'const SDKLegacy'+goName(k)+'='+v+'\n').join('');
resources += 'const CurrentSDKVersion='+JSON.stringify(adapterPolicy.sdkVersion)+'\nconst SDKAdapterDescriptionVersion='+adapterPolicy.descriptionVersion+'\n';
resources += 'const SDKIdentityVersion='+sdkIdentityPolicy.identityVersion+'\nconst SDKIdentityDescriptionVersion='+sdkIdentityPolicy.descriptionVersion+'\n';
resources += 'const ComponentScanInputVersion=6\nconst ComponentScanJobVersion=8\nconst ComponentScanDescriptionVersion=9\n';
const schemaTS = header + '\nexport const SCHEMA_RESOURCES = '+JSON.stringify(Object.fromEntries(entries.map(e=>[e.schema.$id,e.schema])))+' as const;\nexport const SCHEMA_IDS = '+JSON.stringify(Object.fromEntries(roots.map(r=>[r.name,r.id])),null,2)+' as const;\n';

async function emit(relative,content) {
  if (relative.endsWith('.go')) {
    const formatted = spawnSync('gofmt',[],{input:content,encoding:'utf8'});
    if (formatted.status !== 0) throw new Error(formatted.stderr);
    content=formatted.stdout;
  }
  const target=path.join(root,relative);
  if (check) {
    const existing=await readFile(target,'utf8').catch(()=>null);
    if (existing!==content) throw new Error('Generated file differs: '+relative+'; run npm run generate');
  } else {await mkdir(path.dirname(target),{recursive:true});await writeFile(target,content);}
}
await emit('ts/src/types.gen.ts',ts);
await emit('ts/src/schemas.gen.ts',schemaTS);
await emit('go/types.gen.go',go);
await emit('go/codec.gen.go',codec);
await emit('go/schemas.gen.go',resources);
await emit('../code-audit-scanner/src/runtime/contract-baseline.gen.ts',header+'\nexport const CONTRACT_BASELINE_VERSION = '+defs.SchemaVersion.const+' as const;\nexport const SCANNER_DESCRIPTION_VERSION = '+defs.ScannerDescriptionV13.properties.schemaVersion.const+' as const;\nexport const SDK_RESULT_ADAPTER_VERSION = '+adapterPolicy.adapterVersion+' as const;\n');
const matrixText=await readFile(path.join(root,'data/execution-compatibility.json'),'utf8');
const matrix=JSON.parse(matrixText);
if(matrix.matrixVersion!==1 || JSON.stringify(matrix.controlAPIs)!=='[1,2,3,4]' || matrix.profiles.length!==9)throw new Error('Invalid execution compatibility matrix');
const expectedPolicy={discovery:'presence-only',fallbackOn:[404,405],claimFallback:false,unknownContracts:'reject-before-model',history:'preserve-readers',upgradeOrder:['server','agent-and-scanner','web']};
if(Object.keys(matrix.policies).length!==Object.keys(expectedPolicy).length || Object.entries(expectedPolicy).some(([k,v])=>JSON.stringify(matrix.policies[k])!==JSON.stringify(v)))throw new Error('Unsupported compatibility policy; update the consumer and tests before changing it');
for(const [i,p] of matrix.profiles.slice(0,4).entries()){
 if(p.id!==['legacy','input','result','observations'][i] || p.progress!==(i===3?2:0) || p.event!==(i===3?2:0) || p.controlAPI!==i+1 || p.description!==i+1 || p.job!==i || p.input!==(i?1:0) || p.result!==(i>=2?1:0) || p.observation!==(i===3?1:0))throw new Error('Unsupported profile matrix');
}
if(JSON.stringify(matrix.profiles[4])!==JSON.stringify({id:'scan_input',input:2,result:1,observation:1,controlAPI:4,description:5,job:4,progress:2,event:2}))throw new Error('Invalid scan input profile');
if(JSON.stringify(matrix.profiles[5])!==JSON.stringify({id:'sdk_operation',input:3,result:1,observation:1,controlAPI:4,description:6,job:5,progress:2,event:2}))throw new Error('Invalid SDK operation profile');
if(JSON.stringify(matrix.profiles[6])!==JSON.stringify({id:'git_diff',input:4,result:1,observation:1,controlAPI:4,description:7,job:6,progress:2,event:2}))throw new Error('Invalid Git diff profile');
if(JSON.stringify(matrix.profiles[7])!==JSON.stringify({id:'component_planning',input:5,result:1,observation:1,controlAPI:4,description:8,job:7,progress:2,event:2}))throw new Error('Invalid component planning profile');
if(JSON.stringify(matrix.profiles[8])!==JSON.stringify({id:'component_scan',input:6,result:1,observation:1,controlAPI:4,description:9,job:8,progress:2,event:2}))throw new Error('Invalid component scan profile');
const matrixHash=createHash('sha256').update(matrixText).digest('hex');
const matrixHeader='// Generated from data/execution-compatibility.json. Do not edit.\n';
await emit('go/control/matrix.gen.go',matrixHeader+'package control\nconst MatrixVersion=1\nconst MatrixHash='+JSON.stringify(matrixHash)+'\nvar controlAPIs=[]int{'+matrix.controlAPIs.join(',')+'}\nvar profiles=[]Profile{\n'+matrix.profiles.map(p=>' {ID:'+JSON.stringify(p.id)+',Input:'+p.input+',Result:'+p.result+',Observation:'+p.observation+',ControlAPI:'+p.controlAPI+',Description:'+p.description+',Job:'+p.job+',Progress:'+p.progress+',Event:'+p.event+'},').join('\n')+'\n}\n');
await emit('ts/src/matrix.gen.ts',matrixHeader+'export const EXECUTION_MATRIX='+JSON.stringify(matrix,null,2)+' as const;\nexport const EXECUTION_MATRIX_HASH='+JSON.stringify(matrixHash)+' as const;\n');
await emit('COMPATIBILITY.md','# 执行合同兼容矩阵\n\n<!-- Generated from data/execution-compatibility.json. -->\n\n包版本用于诊断；任务派发以实际声明的合同能力为准。HTTP 控制接口版本与文档 schemaVersion 分别管理。\n\n| 任务格式 | 输入 / 结果 / 观察 | 最低控制接口 | Scanner 描述 | Job | 进度 / 事件 |\n| --- | --- | --- | --- | --- | --- |\n'+matrix.profiles.map(p=>'| '+p.id+' | '+p.input+' / '+p.result+' / '+p.observation+' | '+p.controlAPI+' | '+p.description+' | '+(p.job||'旧参数')+' | '+p.progress+' / '+p.event+' |').join('\n')+'\n\n最新 Server 支持控制接口 1–4，并发布矩阵摘要。带摘要的 Server 与 Agent 必须使用同一份矩阵；摘要不一致会阻止模型启动，并上报配套组件诊断，不能据包版本猜测兼容。旧组件缺少运行元数据时，以已声明合同能力判定，缺失的版本显示为未知。历史控制客户端可用 presence 探测旧 Server：仅 404/405 可尝试下一接口；认证、网络和数据错误不降级。领取失败不切换接口，已领取任务的固定合同不降级。不支持的格式在模型启动前拒绝。\n\n升级顺序：Server 消费者及迁移 → 配套 Agent/Scanner → Web。旧 Agent/Scanner 只能领取其支持的任务，普通新 Web 任务保持输入/结果/观察 1/1/1；高级参数和知识文档任务为 2/1/1，使用 base Assignment v1 + ScanInputAssignment v2，绑定同一 execution，再物化 Job v4。SDK 独立操作为 3/1/1，另绑定 SDKOperation v1，物化 Job v5，由 Scanner Description v6 声明；缺少操作 sidecar 或 Job v5 能力会在 SDK 前拒绝。Git 差异任务为 4/1/1，使用 GitDiffAssignment v1 和 Job v6、Description v7，严格绑定冻结 base/head 与源码哈希；旧节点不得领取或降级。组件审计为 6/1/1，绑定 ComponentScanRequest v1 与冻结计划，物化 Job v8、由 Description v9 声明；缺少源码、执行、版本或预算绑定时在 SDK 前拒绝。旧 Server 的已知 profile 无法领取 input=2，不能回退或丢弃新参数。历史任务、结果、日志、覆盖报告和 HTML/PDF 报告读取器保留。未知合同不能推断为兼容。运行诊断每 10 秒上报，30 秒后失效；在线/就绪节点和明确的调度模式分别展示；正数固定槽位仅保留为旧节点元数据。缺少、无效或未知版本的 Scanner 描述会阻止新 Agent 领取任务，并向新版 Server 上报诊断。\n\nSchema 核心字段变化（含严格对象中的新增 optional 字段）必须新增 schemaVersion，先发布消费者，再切换生产者。已有任务及 attempt 的合同组合不修改、不降级。旧 reader 保留；仅在证明无入口引用且没有历史写入需求后移除 adapter。升级后用独立 Fixture 验证任务创建、取消、结果/日志/报告读取和组件组合，不以生成类型通过代替进程验收。\n\n当前 Agent 使用显式协商的无配额调度：Server 必须声明 taskDispatch=unbounded，控制 API 4 的 presence 必须同时声明 dispatchMode=unbounded 与 slots=0。未知或缺失策略不回退到旧 Server；旧正数槽位上报仍可读取。迁移 023 支持该 0 值，任务队列不再受全局/模型/项目配额或组件权重限制。Web 保留旧配置和历史记录读取，退役写入返回 410；effectiveCapacity=null，不虚构容量大数。该策略属于控制面发现与存活上报，不改变历史 Assignment/Job/Result Schema。\n\nSDK 参数侧带为版本 1，由 Description v11 与 Server sdkParametersVersion=1 协商；在保持历史严格模型与 Job Schema 不变的前提下，绑定 execution 传递推理强度和组件知识/指令。未知版本、旧节点和缺失侧带在模型启动前拒绝；历史任务缺少侧带时沿用原默认值。\n\nSDK 执行身份侧带为版本 1，由 ScannerDescription v10 和 Server 发现字段 sdkIdentityVersion=1 共同声明。身份控制队列需最近 30 秒内 ready、矩阵匹配、控制 API 4/已知描述 10、11 或 12 的节点报告；身份任务另核对冻结的 Agent/凭据版本，且 v12 配套节点允许同一身份的父执行并行，凭据快照与写回版本校验仍隔离。缺少身份能力的历史客户端不下发身份字段；当前 Agent 另要求上述无配额策略；旧节点保留历史 profile，不能领取身份任务。此增量不改变上述历史 profile 或严格 Schema。\n\nSDK 原生数量域由 sdkConcurrencyVersion=1 与 Description v12 声明。既有 Worker、子代理、组件并发和发现次数字段只扩宽到安全整数域，字段结构与历史读取不变；超过旧域的任务需显式能力及新鲜 v12 节点上报，不能交给旧节点或自动截断。迁移 024 将数值列扩为 BIGINT；Web 主动配置的预算、超时与范围保留。同身份并行仅对已确认该能力的新节点开放，ChatGPT 使用执行私有凭据快照与原子 CAS 写回。详见 [SDK 原生并发规则](../docs/SDK_NATIVE_CONCURRENCY.md)。\n');
await emit('ts/src/sdk-concurrency.gen.ts',header+'export const SDK_MAX_SAFE_INTEGER = '+concurrencyPolicy.maxSafeInteger+' as const;\nexport const SDK_LEGACY_CONCURRENCY = '+JSON.stringify(concurrencyPolicy.legacy)+' as const;\n');
console.log((check?'Verified':'Generated')+' Go/TS contracts from '+entries.length+' schemas ('+digest.slice(0,12)+')');
