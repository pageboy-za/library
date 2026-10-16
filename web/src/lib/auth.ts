import { writable } from 'svelte/store';
export type StaffUser = {id:number;username:string;display_name:string;role:'administrator'|'librarian'};
export type Session = {user:StaffUser|null;csrf_token?:string;setup_required:boolean};
export const session = writable<Session|null>(null);
let current:Session|null=null;
let pending:Promise<Session>|null=null;
export async function loadSession(refresh=false):Promise<Session>{
 if(current&&!refresh)return current;
 if(pending)return pending;
 pending=(async()=>{
  const response=await fetch('/api/auth/session',{cache:'no-store'});
  const value=await response.json();
  if(!response.ok)throw new Error(value.error||'Could not check your sign-in.');
  current=value;session.set(value);return value as Session;
 })();
 try{return await pending;}finally{pending=null;}
}
// All writes, including multipart uploads, use the server-issued session token.
export async function authenticatedFetch(url:string, options:RequestInit={}):Promise<Response>{
 const method=(options.method||'GET').toUpperCase();
 if(!['GET','HEAD','OPTIONS'].includes(method)){
  const state=await loadSession();
  const headers=new Headers(options.headers);
  if(state.csrf_token)headers.set('X-CSRF-Token',state.csrf_token);
  options={...options,headers};
 }
 const response=await fetch(url,options);
 if(response.status===401&&current?.user){current=null;session.set(null);}
 return response;
}
export async function signOut(){
 const response=await authenticatedFetch('/api/auth/logout',{method:'POST',headers:{'Content-Type':'application/json'},body:'{}'});
 if(!response.ok)throw new Error('Could not sign out. Try again.');
 current=null;session.set(null);await loadSession(true);
}
