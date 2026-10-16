<script lang="ts">
import '../styles/global.css';
import '../styles/app.css';
import { page } from '$app/state';
import {onMount} from 'svelte';
import {loadSession} from '../lib/auth';
onMount(()=>{void loadSession().catch(()=>{});});
import Nav from '../components/global/Nav.svelte';
import favicon from '../assets/library-favicon.svg?url';
let { children } = $props();
const kiosk = $derived(page.url.pathname === '/staff/');
$effect(() => {
  document.body.classList.toggle('library-kiosk', kiosk);
  return () => document.body.classList.remove('library-kiosk');
});
</script>
<svelte:head>
<meta name="description" content="Your books, their shelves and the people reading them."/>
<link rel="icon" type="image/svg+xml" sizes="any" href={favicon}/>
</svelte:head>
{#if kiosk}
<div id="kiosk-search-shell" style="display: contents">{#key page.url.pathname + page.url.search}{@render children()}{/key}</div>
{:else}
<a class="skip-link" href="#main">Skip to main content</a>
<Nav/>
<main id="main" tabindex="-1" class="app-main"><div class="app-content" id={page.url.pathname==='/'?'kiosk-search-shell':undefined}>{#key page.url.pathname + page.url.search}{@render children()}{/key}</div></main>
{/if}
<style>
.skip-link {position:fixed;top:1rem;left:1rem;z-index:100;transform:translateY(-200%)}
.skip-link:focus {transform:translateY(0)}
</style>
