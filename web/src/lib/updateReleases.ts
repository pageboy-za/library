export type UpdateStatus={installed:string;phase?:string;message?:string;version?:string;previous?:string};
function versionParts(value:string):number[]|null{
 if(!/^v\d+\.\d+\.\d+$/.test(value))return null;
 const parts=value.slice(1).split('.').map(Number);
 return parts.every(Number.isSafeInteger)?parts:null;
}
export function rollbackVersion(status:UpdateStatus):string|null{
 // Only a completed installation proves which application was replaced.
 if(status.phase!=='ready'||status.version!==status.installed)return null;
 const match=status.previous?.match(/^\/opt\/home-library\/releases\/(v\d+\.\d+\.\d+)$/);
 return match&&versionParts(match[1])?match[1]:null;
}
export function canInstallRelease(version:string,status:UpdateStatus):boolean{
 const candidate=versionParts(version),installed=versionParts(status.installed);
 if(!candidate||!installed||version===status.installed)return false;
 for(let i=0;i<3;i++){
  if(candidate[i]>installed[i])return true;
  if(candidate[i]<installed[i])return version===rollbackVersion(status);
 }
 return false;
}
