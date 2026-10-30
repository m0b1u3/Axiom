// Producer-side aid only. It never changes a draft, source file or verifier.
import {constants,openSync,closeSync,fstatSync,readFileSync,lstatSync,realpathSync} from 'node:fs';
import path from 'node:path';
import {createHash} from 'node:crypto';
import {fileURLToPath} from 'node:url';

export function extractSource(repository,relative,startLine,endLine) {
 if(typeof relative!=='string'||!relative||/^[A-Za-z]:|^[/\\]|\\|\0/u.test(relative)||relative.split('/').some(p=>!p||p==='.'||p==='..'))throw Error('unsafe source path');
 if(!Number.isSafeInteger(startLine)||!Number.isSafeInteger(endLine)||startLine<1||endLine<startLine)throw Error('invalid line range');
 let file=realpathSync(repository);
 for(const part of relative.split('/')){file=path.join(file,part);if(lstatSync(file).isSymbolicLink())throw Error('symbolic link source');}
 const fd=openSync(file,constants.O_RDONLY|(constants.O_NOFOLLOW??0));
 let raw;
 try{const stat=fstatSync(fd);if(!stat.isFile()||stat.size>4*1024*1024)throw Error('source file unavailable or exceeds 4 MiB');raw=readFileSync(fd);if(raw.length>4*1024*1024)throw Error('source file exceeds 4 MiB');}finally{closeSync(fd);}
 const lines=new TextDecoder('utf-8',{fatal:true}).decode(raw).replaceAll('\r\n','\n').split('\n');
 if(endLine>lines.length)throw Error('line range outside source');
 return {path:relative,startLine,endLine,code:lines.slice(startLine-1,endLine).join('\n'),sourceFileSha256:createHash('sha256').update(raw).digest('hex')};
}

export function checkSourceEvidence(repository,draft) {
 if(!draft||!Array.isArray(draft.findings))throw Error('draft.findings must be an array');
 const diagnostics=[];let checked=0;
 for(const [findingIndex,finding] of draft.findings.entries()){
  if(!Array.isArray(finding?.codeEvidence)){diagnostics.push({findingIndex,error:'codeEvidence must be an array'});continue;}
  for(const [evidenceIndex,evidence] of finding.codeEvidence.entries()){
   checked++;
   try{
    if(typeof evidence?.code!=='string'||!evidence.code.length)throw Error('nonempty code is required');
    // Same CRLF and one terminal-newline handling as Server sourceLines.
    const snippet=evidence.code.replaceAll('\r\n','\n').replace(/\n$/u,'');
    const end=evidence.endLine??(evidence.startLine+snippet.split('\n').length-1);
    const source=extractSource(repository,evidence.path,evidence.startLine,end);
    if(source.code!==snippet)throw Error('code_mismatch: copy complete source lines, preserving indentation and the entire last line');
   }catch(error){diagnostics.push({findingIndex,evidenceIndex,path:evidence?.path??null,startLine:evidence?.startLine??null,error:error.message});}
  }
 }
 return {ok:diagnostics.length===0,checked,diagnostics};
}

// Node canonicalizes the entry module even when argv keeps a /var or symlink
// alias. Compare the same real file so the CLI cannot silently exit unchecked.
const isMain=()=>{try{return !!process.argv[1]&&realpathSync(fileURLToPath(import.meta.url))===realpathSync(process.argv[1]);}catch{return false;}};
if(isMain()){
 try{
  const [command,repository,...args]=process.argv.slice(2);let result;
  if(command==='extract'&&args.length===3)result=extractSource(repository,args[0],Number(args[1]),Number(args[2]));
  else if(command==='check'&&args.length===1){result=checkSourceEvidence(repository,JSON.parse(readFileSync(args[0],'utf8')));if(!result.ok)process.exitCode=1;}
  else throw Error('Usage: extract <repository> <relative-path> <start-line> <end-line> | check <repository> <draft-json>');
  process.stdout.write(JSON.stringify(result)+'\n');
 }catch(error){process.stderr.write(JSON.stringify({ok:false,error:error.message})+'\n');process.exitCode=1;}
}
