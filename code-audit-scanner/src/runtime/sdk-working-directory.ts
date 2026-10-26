import {mkdir} from 'node:fs/promises';
import path from 'node:path';

/** Scanner runs in one process per execution. SDK child commands inherit cwd;
 * keep their bootstrap/config paths in this execution, outside the immutable
 * Scanner installation and source snapshot. No credentials are copied here. */
export async function enterSDKWorkingDirectory(outputPath:string):Promise<()=>void>{
 const previous=process.cwd();
 const directory=path.resolve(outputPath,'runtime','sdk-cwd');
 await mkdir(directory,{recursive:true,mode:0o700});
 // Bubblewrap protects the project config directory during bootstrap. It must
 // already exist when the surrounding container filesystem is read-only.
 await mkdir(path.join(directory,'.codex'),{recursive:true,mode:0o700});
 process.chdir(directory);
 return ()=>process.chdir(previous);
}
