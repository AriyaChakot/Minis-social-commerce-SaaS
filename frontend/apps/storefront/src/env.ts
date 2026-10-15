import { defineEnvVars } from '@sveltejs/kit/env'

// SvelteKit 3: every environment variable the app uses is declared here, with a validator that
// runs when the app starts. A bad value fails the start-up, not some request hours later.
// Variables are private (server-only, `$app/env/private`) unless marked `public: true`.
export const variables = defineEnvVars({
  API_BASE: {
    description: 'Base URL of the Go API, e.g. http://api:8080/api/v1. Only the storefront server calls it.',
    schema: (value) => {
      const url = value || 'http://127.0.0.1:8080/api/v1'
      new URL(url) // throws "Invalid URL" for typos
      return url.replace(/\/+$/, '')
    },
  },
})
