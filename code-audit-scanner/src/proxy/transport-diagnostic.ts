/** Expose only a bounded category, never exception messages, URLs, headers,
 * request content or credential-bearing error objects. */
const causes=new Set(['UND_ERR_INVALID_ARG','UND_ERR_HEADERS_TIMEOUT','UND_ERR_BODY_TIMEOUT','UND_ERR_CONNECT_TIMEOUT','UND_ERR_SOCKET','ECONNRESET','ECONNREFUSED','ETIMEDOUT','ENOTFOUND','EAI_AGAIN','CERT_HAS_EXPIRED','ERR_TLS_CERT_ALTNAME_INVALID']);
export function upstreamTransportDiagnostic(error:unknown,phase:'headers'|'stream',startedAt:number):{code:'upstream_transport_error';message:string;phase:'headers'|'stream';cause:string;elapsedMs:number}{
 const value=error as {code?:unknown;cause?:{code?:unknown}}|null;
 const candidate=value?.cause?.code??value?.code;
 const cause=typeof candidate==='string'&&causes.has(candidate)?candidate:'UNCLASSIFIED_TRANSPORT_ERROR';
 const elapsedMs=Math.max(0,Date.now()-startedAt);
 return {code:'upstream_transport_error',message:`Upstream ${phase} interrupted (${cause}; ${elapsedMs}ms)`,phase,cause,elapsedMs};
}

const responseCodes=new Set(['server_error','upstream_error','upstream_request_failed','rate_limit_exceeded','insufficient_quota','context_length_exceeded','invalid_request_error','authentication_error','permission_denied','upstream_output_truncated','upstream_empty_response','upstream_stream_incomplete','upstream_invalid_sse']);
/** Provider error bodies may echo credentials or source text. Persist only a
 * recognized category, event shape and duration; forward the original to SDK. */
export function upstreamResponseDiagnostic(value:unknown,event:string,startedAt:number):{code:string;event:string;elapsedMs:number}{
 const body=value as {type?:unknown;error?:{code?:unknown};response?:{error?:{code?:unknown}}}|null;
 const candidate=body?.response?.error?.code??body?.error?.code;
 const code=typeof candidate==='string'&&responseCodes.has(candidate)?candidate:'unclassified_upstream_error';
 const kind=body?.type??event;
 return {code,event:typeof kind==='string'&&['response.failed','response.incomplete','error'].includes(kind)?kind:'error',elapsedMs:Math.max(0,Date.now()-startedAt)};
}
