<script lang="ts">
import {api} from '../../../lib/api';
import {goto,beforeNavigate} from '$app/navigation';
import {loadSession,session,signOut} from '../../../lib/auth';
beforeNavigate(navigation=>{if(navigation.from&&busy)navigation.cancel();});
async function logout(){busy=true;error='';try{await signOut();busy=false;await goto('/staff/login/');}catch(e){error=(e as Error).message;}finally{busy=false;}}
let current='';let password='';let confirm='';let busy=false;let error='';let message='';
async function submit(event:SubmitEvent){event.preventDefault();if(busy)return;error='';message='';if(password!==confirm){error='Your new passwords do not match.';return;}busy=true;try{await api('/api/auth/password',{current_password:current,new_password:password});current='';password='';confirm='';await loadSession(true);message='Password changed. Use your new password next time you sign in.';}catch(e){error=(e as Error).message;}finally{busy=false;}}
</script>
<svelte:head><title>Your staff account · Home Library</title></svelte:head>
<h1>Your staff account</h1><p>{$session?.user?.display_name||$session?.user?.username} · {$session?.user?.role}</p>
<p><button class="library-button secondary" disabled={busy} onclick={logout}>Sign out</button></p>
<h2>Change your password</h2><form onsubmit={submit}><label class="library-field">Current password<input type="password" bind:value={current} autocomplete="current-password" required disabled={busy}/></label><label class="library-field">New password<input type="password" bind:value={password} autocomplete="new-password" minlength="12" required disabled={busy}/></label><label class="library-field">Confirm new password<input type="password" bind:value={confirm} autocomplete="new-password" required disabled={busy}/></label>{#if error}<p role="alert" class="library-error">{error}</p>{/if}{#if message}<p role="status">{message}</p>{/if}<button class="library-button" disabled={busy}>{busy?'Saving…':'Change password'}</button></form>
<style>form{display:grid;gap:1rem;max-width:28rem;margin-block:1.5rem}</style>
