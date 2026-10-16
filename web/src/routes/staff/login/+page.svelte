<script lang="ts">
import {onMount} from 'svelte';
import {goto,beforeNavigate} from '$app/navigation';
import {api} from '../../../lib/api';
import {loadSession,session} from '../../../lib/auth';
beforeNavigate(navigation=>{if(navigation.from&&busy)navigation.cancel();});
let username='';let password='';let busy=false;let error='';
onMount(()=>{void loadSession(true).catch(e=>error=e.message);});
async function submit(event:SubmitEvent){event.preventDefault();if(busy)return;busy=true;error='';try{await api('/api/auth/login',{username,password});password='';await loadSession(true);const next=new URLSearchParams(location.search).get('next')||'/staff/';busy=false;await goto(next.startsWith('/staff/')&&!next.startsWith('/staff/login/')?next:'/staff/');}catch(e){error=(e as Error).message;}finally{busy=false;}}
</script>
<svelte:head><title>Staff sign in · Home Library</title></svelte:head>
<h1>Staff sign in</h1><p>Use your own administrator or librarian account.</p>
{#if $session?.setup_required}<p role="status">This library needs its first administrator account. Complete setup on the server before signing in.</p>{:else}
<form class="auth-form" onsubmit={submit}><label class="library-field">Username<input bind:value={username} autocomplete="username" required disabled={busy}/></label><label class="library-field">Password<input type="password" bind:value={password} autocomplete="current-password" required disabled={busy}/></label>{#if error}<p role="alert" class="library-error">{error}</p>{/if}<button class="library-button" disabled={busy}>{busy?'Signing in…':'Sign in'}</button></form>
{/if}
<p><a href="/">Back to the library</a></p>
<style>.auth-form{display:grid;gap:1rem;max-width:28rem;margin-block:2rem}</style>
