export type MembershipStatus='pending'|'approved'|'rejected'|'suspended';
export type Registration={name:string;status:MembershipStatus;card_code?:string};
export type MembershipReceipt={registration:Registration|null;wallet:{apple:boolean;google:boolean};csrf_token?:string};
export async function membershipRequest<T>(path:string,body?:unknown,csrfToken?:string):Promise<T>{
 const headers=new Headers();
 if(body!==undefined){
  if(!csrfToken)throw new Error('Refresh this page before trying again.');
  headers.set('Content-Type','application/json');headers.set('X-Member-CSRF',csrfToken);
 }
 const response=await fetch(path,{credentials:'same-origin',cache:'no-store',...(body===undefined?{}:{method:'POST',headers,body:JSON.stringify(body)})});
 const value=await response.json();
 if(!response.ok)throw new Error(value.error||'Could not complete your request.');
 return value as T;
}
export function googleWalletURL(value:string):string{
 const url=new URL(value);
 if(url.protocol!=='https:'||url.hostname!=='pay.google.com'||url.port||url.username||url.password||!url.pathname.startsWith('/gp/v/save/'))throw new Error('The library returned an invalid Google Wallet link. Ask a librarian for help.');
 return url.href;
}
