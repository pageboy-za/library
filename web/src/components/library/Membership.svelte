<script lang="ts">
import {onMount,onDestroy} from 'svelte';
import {beforeNavigate} from '$app/navigation';
import {membershipRequest,googleWalletURL,type MembershipReceipt} from '../../lib/membership';
export let joining=false;
let receipt:MembershipReceipt|null=null;let loading=true;let busy=false;let error='';let message='';let name='';let email='';let telephone='';let disposed=false;let qrError=false;
beforeNavigate(navigation=>{if(navigation.from&&busy)navigation.cancel();});
onMount(()=>{void refresh();});onDestroy(()=>{disposed=true;});
async function refresh(){loading=true;error='';try{const value=await membershipRequest<MembershipReceipt>('/api/member/registration');if(!disposed){receipt=value;qrError=false;}}catch(e){if(!disposed)error=(e as Error).message;}finally{if(!disposed)loading=false;}}
async function submit(event:SubmitEvent){event.preventDefault();if(busy)return;busy=true;error='';message='';try{const value=await membershipRequest<MembershipReceipt>('/api/member/registration',{name:name.trim(),email:email.trim(),telephone:telephone.trim()},receipt?.csrf_token);if(!disposed){receipt=value;email='';telephone='';message='Your request has been sent to the library team.';}}catch(e){if(!disposed)error=(e as Error).message;}finally{if(!disposed)busy=false;}}
async function google(){if(busy)return;busy=true;error='';try{const result=await membershipRequest<{url:string}>('/api/member/card/google',{},receipt?.csrf_token);if(!disposed){const destination=googleWalletURL(result.url);busy=false;location.assign(destination);}}catch(e){if(!disposed)error=(e as Error).message;}finally{if(!disposed)busy=false;}}
</script>
<svelte:head><title>{joining?'Join the library':'Your library card'} · Home Library</title></svelte:head>
<div class="membership">
<h1>{joining?'Join the library':'Your library card'}</h1>
{#if error}<p class="library-error" role="alert">{error}</p>{/if}{#if message}<p role="status">{message}</p>{/if}
{#if loading}<p role="status">Checking your membership…</p>{:else if !receipt}<button class="library-button secondary" onclick={refresh}>Try again</button>
{:else if !receipt.registration}
 {#if joining}
 <p>Leave your details and a librarian will review your request. Your card becomes available after approval.</p>
 <form onsubmit={submit}><label class="library-field">Your name<input bind:value={name} autocomplete="name" maxlength="100" required disabled={busy}/></label><label class="library-field">Email address<input type="email" bind:value={email} autocomplete="email" maxlength="320" required disabled={busy}/></label><label class="library-field">Telephone <small>Optional</small><input type="tel" bind:value={telephone} autocomplete="tel" maxlength="40" disabled={busy}/></label><p class="library-help">Library staff can see these details to manage your membership. Submitting this form does not verify your email address or telephone number.</p><button class="library-button" disabled={busy||!receipt.csrf_token}>{busy?'Sending…':'Request membership'}</button>{#if !receipt.csrf_token}<p role="status">Refresh this page to start a secure registration.</p><button type="button" class="library-button secondary" onclick={refresh}>Refresh</button>{/if}</form>
 {:else}<p>No membership request is saved in this browser.</p><a class="library-button" href="/join/">Join the library</a><p>If you already have a card, ask a librarian for help. Do not submit another request to recover it.</p>{/if}
{:else}
 <p class="member-name">{receipt.registration.name}</p>
 {#if receipt.registration.status==='pending'}<h2>Waiting for approval</h2><p>A librarian needs to approve your request before you can use a card. This page does not send an approval email.</p>
 {:else if receipt.registration.status==='rejected'}<h2>Request not approved</h2><p>Speak to a librarian if you have questions about your membership request.</p>
 {:else if receipt.registration.status==='suspended'}<h2>Membership paused</h2><p>Your card is unavailable while your membership is suspended. Ask a librarian for help.</p>
 {:else if receipt.registration.status==='approved'}
 <section class="library-card" aria-label="Your approved library card"><h2>Library member</h2><p class="card-name">{receipt.registration.name}</p>{#if receipt.registration.card_code}<p class="card-code">{receipt.registration.card_code}</p>{#key receipt.registration.card_code}{#if !qrError}<img class="card-qr" src="/api/member/card/qr" alt="Your library card QR code. Scan this after choosing Lend to borrow a book." onerror={()=>qrError=true}/>{/if}{/key}<p>Choose a book, tap Lend, then scan this card to borrow it. A librarian can also help you borrow.</p>{#if qrError}<p role="alert">The QR code could not be loaded. Refresh your card or show your card number to a librarian.</p>{:else}<a class="library-button secondary" href="/api/member/card/qr" download="library-card-qr.png">Download your QR code</a>{/if}{:else}<p role="status">Your membership is approved. Refresh to load your card.</p>{/if}</section>
 <section class="wallet"><h2>Keep your card on your phone</h2>{#if receipt.wallet.apple}<a class="library-button" href="/api/member/card/apple" download>Add to Apple Wallet</a>{:else}<p>Apple Wallet is not configured for this library. You can use the card and QR code above.</p>{/if}{#if receipt.wallet.google}<button class="library-button" disabled={busy||!receipt.csrf_token} onclick={google}>{busy?'Preparing your card…':'Add to Google Wallet'}</button><p class="library-help">Adding a card to Google Wallet needs internet access and a Google account.</p>{:else}<p>Google Wallet is not configured for this library. You can use the card and QR code above.</p>{/if}</section>
 {/if}
 <button class="library-button secondary" disabled={busy} onclick={refresh}>Refresh membership status</button>
 <p><a href="/membership/">Your library card</a></p>
{/if}
<p class="library-help">Use the library’s Wi-Fi to register or refresh this page. Return to <a href="/membership/">Your library card</a> in the same browser. This browser’s access lasts up to 30 days; clearing its cookies removes access. Staff approval and wallet cards do not provide an online member account.</p>
<p><a href="/">Browse the library</a></p>
</div>
<style>.membership{max-width:42rem;min-width:0;display:grid;gap:1rem;overflow-wrap:anywhere}.membership h1{font-family:Georgia,serif;font-size:clamp(2rem,6vw,3rem)}.membership h2{font-size:1.4rem}.membership form{display:grid;gap:1.25rem;margin-block:1rem}.member-name{font-size:1.25rem}.library-card{display:grid;gap:1rem;background:var(--app-surface);border:1px solid var(--app-line);border-radius:.5rem;padding:1.5rem;max-width:30rem}.card-name{font-size:1.6rem;font-weight:600}.card-code{font-family:monospace;overflow-wrap:anywhere;font-size:.9rem}.card-qr{width:240px;max-width:100%;height:auto;background:white;padding:.5rem}.wallet{display:grid;gap:1rem;margin-block:1rem}.membership .library-button{justify-self:start;max-width:100%;white-space:normal;text-align:center}.membership p{line-height:1.6}</style>
