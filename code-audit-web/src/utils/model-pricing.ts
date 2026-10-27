import {validate} from '../contracts';
import type {ModelPricing,TokenCostEstimate} from '../contracts';

export function emptyModelPricing():ModelPricing {
 return {schemaVersion:1,currency:'USD',unit:'per_million_tokens',cacheMode:'cache_read',inputPerMillion:null,cacheReadPerMillion:null,cacheWritePerMillion:null,outputPerMillion:null,note:''};
}
export function checkedModelPricing(value:ModelPricing|null):ModelPricing|null {
 if(value===null)return null;
 const result=structuredClone(value);validate('ModelPricing',result);return result;
}
export function tokenEstimateView(value:unknown,taskId:number,attempt:number):TokenCostEstimate|null {
 try {validate('TokenCostEstimate',value,{taskId:String(taskId),attempt});return value;}catch{return null}
}
export function pricingMoney(amount:number|null|undefined,currency:string|null|undefined):string {
 if(amount==null||!currency)return '未知';
 const n=BigInt(amount),fraction=String(n%1000000n).padStart(6,'0').replace(/0+$/,'');
 return currency+' '+String(n/1000000n)+(fraction?'.'+fraction:'');
}
export const pricingReasons:Record<string,string>={
 price_missing:'本次执行未配置所需单价',usage_missing:'未取得本次执行用量',usage_incomplete:'仅有部分用量，完整费用未知',
 usage_regressed:'累计用量发生回退，保留已知下界',usage_invalid:'用量计数不一致',price_invalid:'单价格式无效',
 cache_read_usage_missing:'未上报缓存读取用量',cache_write_usage_missing:'未上报缓存写入用量',
 amount_out_of_range:'金额超出可保存范围',model_config_missing:'执行时模型配置已不存在',
 model_price_binding_mismatch:'当前配置的接口或模型与冻结执行不同，未套用其单价',
};
