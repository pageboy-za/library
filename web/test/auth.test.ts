import test from 'node:test';
import assert from 'node:assert/strict';
import {get} from 'svelte/store';
import {authenticatedFetch,loadSession,session,signOut} from '../src/lib/auth.ts';

// These tests exercise the actual request helper used by JSON writes, lending,
// cover uploads and updates. They never connect to an installation.
test('session token applies to writes and session expiry closes the client gate',async()=>{
 const originalFetch=globalThis.fetch;
 let authenticated=true;
 let token='token-one';
 const requests:{url:string;options:RequestInit}[]=[];
 globalThis.fetch=async(input,options={})=>{
  const url=String(input);requests.push({url,options});
  if(url==='/api/auth/session')return Response.json(authenticated?{user:{id:1,username:'staff',display_name:'Staff',role:'administrator'},csrf_token:token,setup_required:false}:{user:null,setup_required:false});
  if(url==='/api/auth/logout'){authenticated=false;return Response.json({ok:true});}
  if(url==='/api/expired')return Response.json({error:'Sign in required'},{status:401});
  return Response.json({ok:true});
 };
 try{
  // Safe reads do not bootstrap authentication or add a token.
  await authenticatedFetch('/api/member/books');
  assert.equal(requests.length,1);
  await loadSession(true);
  await authenticatedFetch('/api/books',{method:'POST',headers:{'Content-Type':'application/json'},body:'{}'});
  let write=requests.at(-1)!;
  assert.equal(new Headers(write.options.headers).get('X-CSRF-Token'),'token-one');
  assert.equal(new Headers(write.options.headers).get('Content-Type'),'application/json');
  // Multipart requests preserve the browser-generated content type/boundary.
  const upload=new FormData();upload.append('image',new Blob(['image']),'cover.png');
  await authenticatedFetch('/api/books/1/covers/upload',{method:'POST',body:upload});
  write=requests.at(-1)!;
  assert.equal(new Headers(write.options.headers).get('X-CSRF-Token'),'token-one');
  assert.equal(new Headers(write.options.headers).get('Content-Type'),null);
  assert.equal(write.options.body,upload);
  // Login/session renewal must replace the token used for subsequent writes.
  token='token-two';await loadSession(true);
  await authenticatedFetch('/api/staff/accounts/2',{method:'PATCH',body:'{}'});
  assert.equal(new Headers(requests.at(-1)!.options.headers).get('X-CSRF-Token'),'token-two');
  let latest:unknown;
  const unsubscribe=session.subscribe(value=>latest=value);
  await authenticatedFetch('/api/expired');
  assert.equal(latest,null);
  // Sign out invalidates the client session and refreshes the anonymous state.
  await loadSession(true);await signOut();
  assert.equal(get(session)?.user,null);
  const logout=requests.find(request=>request.url==='/api/auth/logout')!;
  assert.equal(new Headers(logout.options.headers).get('X-CSRF-Token'),'token-two');
  unsubscribe();
 }finally{globalThis.fetch=originalFetch;}
});
