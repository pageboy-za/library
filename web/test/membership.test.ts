import test from 'node:test';
import assert from 'node:assert/strict';
import {membershipRequest,googleWalletURL,type MembershipReceipt} from '../src/lib/membership.ts';

test('registration and Wallet requests use member CSRF and cookie credentials without staff auth',async()=>{
 const originalFetch=globalThis.fetch;
 const requests:{url:string;options:RequestInit}[]=[];
 globalThis.fetch=async(input,options={})=>{
  requests.push({url:String(input),options});
  return Response.json({registration:{name:'Example Reader',status:'pending'},wallet:{apple:false,google:false},csrf_token:'member-form-token'});
 };
 try{
  await assert.rejects(()=>membershipRequest('/api/member/registration',{name:'Example Reader'}),/Refresh/);
  assert.equal(requests.length,0,'writes must not be sent without member CSRF');
  const receipt=await membershipRequest<MembershipReceipt>('/api/member/registration');
  assert.equal(requests.length,1,'member flow must not request a staff session');
  assert.equal(requests[0].url,'/api/member/registration');
  assert.equal(requests[0].options.credentials,'same-origin');
  assert.equal(requests[0].options.cache,'no-store');
  await membershipRequest('/api/member/registration',{name:'Example Reader',email:'reader@example.invalid',telephone:''},receipt.csrf_token);
  await membershipRequest('/api/member/card/google',{},receipt.csrf_token);
  for(const request of requests.slice(1)){
   const headers=new Headers(request.options.headers);
   assert.equal(request.options.method,'POST');
   assert.equal(request.options.credentials,'same-origin');
   assert.equal(headers.get('X-Member-CSRF'),'member-form-token');
   assert.equal(headers.get('X-CSRF-Token'),null,'staff session tokens must not enter the member flow');
   assert.equal(headers.get('Content-Type'),'application/json');
   assert.equal(request.url.includes('?'),false,'receipt and CSRF tokens must not be placed in URLs');
  }
 }finally{globalThis.fetch=originalFetch;}
});

test('Wallet continuation permits only Google HTTPS save links',()=>{
 assert.equal(googleWalletURL('https://pay.google.com/gp/v/save/example-pass'),'https://pay.google.com/gp/v/save/example-pass');
 for(const value of ['javascript:alert(1)','http://pay.google.com/gp/v/save/pass','https://pay.google.com.example.invalid/gp/v/save/pass','https://example.invalid/gp/v/save/pass','https://pay.google.com/other','https://user:password@pay.google.com/gp/v/save/pass','https://pay.google.com:8443/gp/v/save/pass'])assert.throws(()=>googleWalletURL(value));
});

test('registration receipt preserves available Wallet options and membership status',async()=>{
 const originalFetch=globalThis.fetch;
 try{
  for(const status of ['pending','approved','rejected','suspended'] as const){
   globalThis.fetch=async()=>Response.json({registration:{name:'Example Reader',status,...(status==='approved'?{card_code:'mbr_example'}:{})},wallet:{apple:status==='approved',google:status==='approved'},csrf_token:'receipt-token'});
   const receipt=await membershipRequest<MembershipReceipt>('/api/member/registration');
   assert.equal(receipt.registration?.status,status);
   assert.deepEqual(receipt.wallet,{apple:status==='approved',google:status==='approved'});
   assert.equal(Boolean(receipt.registration?.card_code),status==='approved');
  }
 }finally{globalThis.fetch=originalFetch;}
});

test('registration throttling and unavailable Wallet configuration surface server errors without retrying',async()=>{
 const originalFetch=globalThis.fetch;
 let calls=0;
 try{
  globalThis.fetch=async()=>{calls++;return Response.json({error:'Too many attempts. Try again in ten minutes.'},{status:429});};
  await assert.rejects(()=>membershipRequest('/api/member/registration',{name:'Example Reader'},'form-token'),/Try again in ten minutes/);
  assert.equal(calls,1,'registration must not automatically retry a throttled submission');
  globalThis.fetch=async()=>Response.json({error:'Google Wallet is not configured for this library.'},{status:503});
  await assert.rejects(()=>membershipRequest('/api/member/card/google',{},'receipt-token'),/not configured/);
 }finally{globalThis.fetch=originalFetch;}
});
