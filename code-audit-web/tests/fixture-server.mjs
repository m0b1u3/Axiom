// Local-only UI lifecycle fixture. Never forwards requests or runs a scan.
import { createServer } from 'node:http';
let retried=false, reads=0;
const finding={id:101,scanId:1,title:'验收用 SQL 注入记录',severity:'high',filePath:'sample.php',startLine:5,endLine:5,cwe:['CWE-89'],confidence:0.9,status:'open',description:'本地验收数据，不是真实扫描结果。',evidence:'query($input)',remediation:'使用参数绑定',raw:JSON.stringify({title:'Local fixture',validation:{method:'fixture'}})};
const task=()=>({id:1,scanNo:'LOCAL-UI-ACCEPTANCE',sourcePath:'fixture-snapshot',provider:'custom',model:'local-fixture',baseUrl:'',scanMode:'standard',timeoutMinutes:30,status:retried?(reads<2?'pending':reads<4?'scanning':'completed'):'completed',currentStage:reads<4?'analysis':'completed',progressJson:'',findingCount:retried&&reads<4?0:1,errorMessage:'',projectId:1,projectName:'本地验收',createdAt:new Date().toISOString(),updatedAt:new Date().toISOString()});
const server=createServer(async(req,res)=>{
 const path=new URL(req.url,'http://localhost').pathname.replace('/api/v1','');
 let data;
 if(path==='/auth/login')data={token:'local-test-only',user:{id:1,username:'local-review'}};
 else if(path==='/auth/me')data={id:1,username:'local-review'};
 else if(path==='/scans/1/retry'){retried=true;reads=0;data=task();console.log('Retry accepted locally');}
 else if(path==='/scans/1'){if(retried)reads++;data=task();console.log('Scan read:',data.status);}
 else if(path==='/scans/1/findings')data=task().status==='completed'?[finding]:[];
 else if(path==='/scans/1/logs')data={events:[],nextAfter:0,total:0};
 else if(path==='/scans/1/compare')data={previousScanId:0,added:[],removed:[],unchanged:[]};
 else if(path==='/scans')data=[task()];
 else if(path==='/projects')data=[];
 else {res.writeHead(404,{'Content-Type':'application/json'});res.end(JSON.stringify({message:'Local fixture route unavailable'}));return;}
 res.writeHead(200,{'Content-Type':'application/json'});res.end(JSON.stringify({data}));
});
server.listen(4319,'127.0.0.1',()=>console.log('Local-only UI fixture: 127.0.0.1:4319'));
