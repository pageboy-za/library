<script lang="ts">
import {onDestroy, tick} from 'svelte';
import {beforeNavigate} from '$app/navigation';
import {membershipRequest} from '../../lib/membership';

export let bookId:number;
export let title:string;
export let available:number;
export let complete:()=>void=()=>{};

let scanning=false;
let preparing=false;
let busy=false;
let code='';
let csrf='';
let error='';
let success=false;
let disposed=false;
let input:HTMLInputElement;
let lendButton:HTMLButtonElement;
let pending:{book_id:number;card_code:string;request_key:string}|null=null;

onDestroy(()=>{disposed=true;code='';pending=null;});
beforeNavigate(navigation=>{if(busy||pending){navigation.cancel();error='Keep this book open until the loan is confirmed. Use Retry safely.';}});
function beforeUnload(event:BeforeUnloadEvent){if(busy||pending){event.preventDefault();event.returnValue='';}}

async function start(){
 if(preparing||busy||pending)return;
 preparing=true;error='';success=false;code='';
 try{
  const value=await membershipRequest<{csrf_token:string}>('/api/member/registration');
  if(disposed)return;
  if(!value.csrf_token)throw new Error('Could not prepare the scanner. Try Lend again.');
  csrf=value.csrf_token;scanning=true;
  await tick();input?.focus();
 }catch(e){if(!disposed)error=(e as Error).message;}
 finally{if(!disposed)preparing=false;}
}
async function cancel(){
 if(busy||pending)return;
 scanning=false;code='';csrf='';error='';
 await tick();lendButton?.focus();
}
async function scan(event:SubmitEvent){
 event.preventDefault();if(busy||pending)return;
 const pass=code.trim();code='';
 if(!/^mbr_[A-Za-z0-9_-]{32}$/.test(pass)){error='Scan the QR code on your library pass, rather than a book barcode.';input?.focus();return;}
 pending={book_id:bookId,card_code:pass,request_key:crypto.randomUUID()};
 await save();
}
async function save(){
 if(!pending||busy)return;
 busy=true;error='';
 try{
  const response=await fetch('/api/member/loans',{
   method:'POST',credentials:'same-origin',cache:'no-store',
   headers:{'Content-Type':'application/json','X-Member-CSRF':csrf},
   body:JSON.stringify(pending)
  });
  const result=await response.json().catch(()=>null);
  if(disposed)return;
  if(response.ok&&result?.borrowed===true){
   pending=null;scanning=false;csrf='';success=true;complete();
  }else if(response.status>=400&&response.status<500&&response.status!==429){
   pending=null;
   error=result?.error||'The loan was declined. Ask a librarian for help.';
   // A fresh scan is required after a definitive rejection.
   scanning=false;csrf='';complete();
  }else{
   error=result?.error||'The loan could not be confirmed. Keep this page open and retry safely.';
  }
 }catch{if(!disposed)error='The loan could not be confirmed. Keep this page open and retry safely.';}
 finally{if(!disposed)busy=false;}
}
</script>

<svelte:window onbeforeunload={beforeUnload}/>
<section class="member-lending" aria-label="Borrow this book" aria-busy={preparing||busy}>
 {#if success}
  <p class="confirmation" role="status">You’ve borrowed <strong>{title}</strong>. You’re ready to take it away.</p>
  <a class="library-button secondary" href="/">Choose another book</a>
 {:else if scanning}
  <h2>Scan your pass</h2>
  <p>Scan the QR code on your library card or Wallet pass to borrow <strong>{title}</strong>.</p>
  <form onsubmit={scan}>
   <label class="library-field">Your library pass
    <input bind:this={input} bind:value={code} disabled={busy||!!pending} autocomplete="off" autocapitalize="off" spellcheck="false" maxlength="100" placeholder="Scan your pass here" oninput={()=>error=''}/>
   </label>
   {#if !pending}<button class="library-button" disabled={busy||!code.trim()}>Borrow book</button>{/if}
  </form>
  <p class="library-help">Use a QR scanner that enters the code and presses Enter. You can also enter your card code and tap Borrow book. Scanning completes the loan.</p>
  {#if !pending}<button class="library-button secondary" type="button" onclick={cancel} disabled={busy}>Cancel</button>{/if}
 {:else if available>0}
  <button class="library-button" bind:this={lendButton} onclick={start} disabled={preparing}>{preparing?'Preparing scanner…':'Lend'}</button>
  <p class="library-help">Choose Lend, then scan your approved library pass to borrow this book.</p>
 {:else}
  <p class="library-help">No copy is available to borrow at the moment.</p>
 {/if}
 {#if error}<p class="library-error" role="alert">{error}</p>{/if}
 {#if pending}<button class="library-button" type="button" disabled={busy} onclick={save}>{busy?'Confirming loan…':'Retry safely'}</button>{/if}
</section>

<style>
.member-lending{display:grid;gap:1rem;min-width:0;padding:1.25rem;border:1px solid var(--app-line);border-radius:.5rem;background:var(--app-surface);margin-block:1.5rem}
.member-lending h2{font-size:1.4rem;margin:0}.member-lending p{margin:0;line-height:1.6;overflow-wrap:anywhere}.member-lending form{display:grid;gap:1rem;min-width:0}.member-lending .library-button{max-width:100%;white-space:normal;text-align:center;justify-self:start}.member-lending input{box-sizing:border-box;min-width:0}.confirmation{color:var(--app-accent)}
</style>
