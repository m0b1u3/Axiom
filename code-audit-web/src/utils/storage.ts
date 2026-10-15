/** Human units never turn a known small file into a displayed zero. Null and
 * invalid observations remain unknown; byte precision remains in the raw DTO. */
export function formatStorageBytes(value:number|bigint|null|undefined):string{
 if(value==null||typeof value==='number'&&(!Number.isSafeInteger(value)||value<0)||typeof value==='bigint'&&value<0n)return '未知';
 const units=['B','KiB','MiB','GiB','TiB','PiB'];let amount=Number(value),index=0;
 while(amount>=1024&&index<units.length-1){amount/=1024;index++}
 return new Intl.NumberFormat('zh-CN',{maximumFractionDigits:index===0?0:2}).format(amount)+' '+units[index];
}
