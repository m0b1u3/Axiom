/** Public diagnostics are fixed codes, never native output, URLs or account data. */
export function identityErrorCode(error: unknown): string {
 const seen=new Set<unknown>();let text='';
 while(error instanceof Error&&!seen.has(error)&&seen.size<5){seen.add(error);text+=' '+error.message+' '+String((error as NodeJS.ErrnoException).code??'');error=error.cause;}
 if(/unsupported_country|region.*not supported|country.*not supported/i.test(text))return 'sdk_auth_region_rejected';
 if(/\b403\b|forbidden/i.test(text))return 'sdk_auth_forbidden';
 if(/\b429\b|too many requests/i.test(text))return 'sdk_auth_rate_limited';
 if(/certificate|CERT_|TLS handshake|SSL/i.test(text))return 'sdk_auth_tls_failed';
 if(/timed? ?out|ETIMEDOUT|timeout/i.test(text))return 'sdk_auth_timeout';
 if(/ENOTFOUND|EAI_AGAIN|dns error|failed to lookup/i.test(text))return 'sdk_auth_dns_failed';
 if(/ECONNREFUSED|ECONNRESET|ENETUNREACH|EHOSTUNREACH|error sending request|connection.*(?:failed|refused|reset)|network.*unreachable/i.test(text))return 'sdk_auth_connection_failed';
 if(/ENOENT|codex.*(?:not found|missing)|executable.*(?:not found|missing)/i.test(text))return 'sdk_auth_runtime_missing';
 if(/SDKDeviceInstructions/.test(text))return 'sdk_auth_instructions_invalid';
 return 'sdk_auth_failed';
}
