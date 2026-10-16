import type { PageLoad } from './$types';
export const prerender = false;
export const ssr = false;
export const load: PageLoad = ({ params }) => ({ id: params.id });
