<script lang="ts">
import {onDestroy,onMount} from 'svelte';
import {api} from '../../lib/api';
export let disabled=false;
export let focusOnMount=false;
export let completesLoan=false;
export let selected:(member:{borrowerId:number;name:string;code:string}|null)=>void;
export let busyChanged:(busy:boolean)=>void=()=>{};
let code='';let reading=false;let error='';let disposed=false;
let input:HTMLInputElement;
onMount(()=>{if(focusOnMount)input?.focus();});
onDestroy(()=>{disposed=true;busyChanged(false);});
function edited(){error='';selected(null);}
async function scan(event:SubmitEvent){
 event.preventDefault();if(disabled||reading)return;
 const value=code.trim();selected(null);error='';
 if(!/^mbr_[A-Za-z0-9_-]{32}$/.test(value)){error='Scan the QR code on a library member card, not a book barcode.';return;}
 reading=true;busyChanged(true);
 try{
  const {member}=await api<{member:{borrower_id?:number;name:string;status:string}}>('/api/members/lookup',{card_code:value});
  if(disposed)return;
  if(member.status!=='approved'||!member.borrower_id)throw new Error('This member card is not available for borrowing.');
  selected({borrowerId:member.borrower_id,name:member.name,code:value});code='';
 }catch(e){if(!disposed)error=(e as Error).message;}
 finally{if(!disposed){reading=false;busyChanged(false);}}
}
</script>
<div class="member-card-scanner">
 <form onsubmit={scan}>
  <label class="library-field">Scan member pass
   <input bind:this={input} bind:value={code} oninput={edited} disabled={disabled||reading} autocomplete="off" autocapitalize="off" spellcheck="false" maxlength="100" placeholder="Click here, then scan the Wallet QR code"/>
  </label>
  <button type="submit" class="library-button secondary" disabled={disabled||reading||!code.trim()}>{reading?'Checking card…':completesLoan?'Borrow book':'Find member'}</button>
 </form>
 <p class="library-help">{completesLoan?'Scan the QR code on the member’s library card or Wallet pass to complete the loan. You can also enter the code and choose Borrow book.':'Use a QR scanner that types the code and presses Enter. Scanning selects the member; confirm the loan below.'}</p>
 {#if error}<p role="alert" class="library-error">{error}</p>{/if}
</div>
<style>.member-card-scanner{display:grid;gap:.5rem;min-width:0}.member-card-scanner form{display:flex;flex-wrap:wrap;gap:.75rem;align-items:end}.member-card-scanner label{flex:1;min-width:min(16rem,100%)}input{width:100%;box-sizing:border-box}.member-card-scanner p{margin:0;font-size:.875rem;line-height:1.5}</style>
