import type {ComponentScanRequest, ComponentScanReceipt} from './types.gen.js';

// Reserve every selected component before dispatch. Unspent allocations are
// returned only when this parent attempt settles, never reused concurrently.
export function componentAllocations(request:ComponentScanRequest):number[] {
  const count=request.selectedIndices.length, each=Math.floor(request.grantMicros/count), extra=request.grantMicros%count;
  return request.selectedIndices.map((_,i)=>Math.min(request.componentLimitMicros,each+(i<extra?1:0)));
}
export function componentReserved(receipts:ComponentScanReceipt[]):number {
  return receipts.reduce((total,r)=>total+(['pending','running'].includes(r.status)?Math.max(0,r.allocatedMicros-(r.observedMicros??0)):0),0);
}
