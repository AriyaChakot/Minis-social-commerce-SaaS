import adapter from '@sveltejs/adapter-node'
import { sveltekit } from '@sveltejs/kit/vite'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [
    tailwindcss(),
    // SvelteKit 3: configuration lives here (svelte.config.js is no longer read).
    // adapter-node builds a standalone Node server (`node build`), ready for a Docker image.
    sveltekit({ adapter: adapter() }),
  ],
})
