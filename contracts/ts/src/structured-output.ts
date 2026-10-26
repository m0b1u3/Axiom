import {decode,validate} from './index.js';
import type {StructuredDiagnosticCatalog,StructuredDiagnosticRequest,StructuredDiagnosticRecord,StructuredDiagnosticRequestPreview,StructuredDiagnosticReplay,StructuredDiagnosticCheck} from './types.gen.js';
import {STRUCTURED_DIAGNOSTIC_CATALOG_JSON,STRUCTURED_DIAGNOSTIC_CATALOG_HASH} from './structured-output.gen.js';
import {validationReportBodyState} from './validation-report.js';

export function structuredDiagnosticCatalog():StructuredDiagnosticCatalog {
 return decode('StructuredDiagnosticCatalog',STRUCTURED_DIAGNOSTIC_CATALOG_JSON);
}
/** Offline evaluation of a fixed fixture. No fetch, SDK, credentials, model,
 * arbitrary prompt/response, source path or transport fallback is accepted. */
export function checkStructuredDiagnosticReply(request:StructuredDiagnosticRequestPreview,replay:StructuredDiagnosticReplay):StructuredDiagnosticCheck {
 const result:StructuredDiagnosticCheck={profile:request.profile,requestSha256:request.bodySha256,formatSha256:request.formatSha256,responseSha256:replay.responseSha256,transport:replay.transport,httpStatus:replay.httpStatus,jsonValid:null,schemaValid:null,bodyState:'not_evaluated',disposition:null,errorCode:null};
 if(replay.transport==='http_error'){result.errorCode='upstream_http_error';return result;}
 if(replay.transport!=='completed'){result.errorCode='transport_incomplete';return result;}
 let value:unknown;try{value=JSON.parse(replay.responseJson);}catch{result.jsonValid=false;result.errorCode='response_json_invalid';return result;}
 result.jsonValid=true;
 const v=value as Record<string,unknown>|null;
 const valid=!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).sort().join(',')==='disposition,report'&&typeof v.disposition==='string'&&['reportable','suppressed','not_applicable','deferred'].includes(v.disposition)&&typeof v.report==='string'&&v.report.length>0;
 result.schemaValid=valid;
 if(!valid){result.errorCode='response_schema_invalid';return result;}
 result.disposition=v!.disposition as NonNullable<StructuredDiagnosticCheck['disposition']>;
 result.bodyState=validationReportBodyState(v!.report as string);
 if(result.bodyState!=='body_present')result.errorCode='report_body_incomplete';
 return result;
}
export function runStructuredDiagnostic(request:StructuredDiagnosticRequest,id:string,createdAt:string):StructuredDiagnosticRecord {
 validate('StructuredDiagnosticRequest',request);
 if(request.protocol!=='responses')throw new Error('Chat diagnostics were removed; only Responses is supported');
 if(request.catalogSha256!==STRUCTURED_DIAGNOSTIC_CATALOG_HASH)throw new Error('diagnostic_catalog_changed');
 const catalog=structuredDiagnosticCatalog(),replay=catalog.scenarios.find(s=>s.id===request.scenario)!;
 const result:StructuredDiagnosticRecord={schemaVersion:1,id,createdAt,sdkVersion:'0.5.0',mode:'offline',catalogSha256:STRUCTURED_DIAGNOSTIC_CATALOG_HASH,sourceSha256:catalog.fixture.sourceSha256,protocol:request.protocol,scenario:request.scenario,upstreamContacted:false,providerCompatibility:'not_tested',model:'not_loaded',inputTokens:0,outputTokens:0,estimatedUsd:0,credentialLoaded:false,results:catalog.requests.filter(r=>r.protocol===request.protocol).map(r=>checkStructuredDiagnosticReply(r,replay))};
 validate('StructuredDiagnosticRecord',result);return result;
}
