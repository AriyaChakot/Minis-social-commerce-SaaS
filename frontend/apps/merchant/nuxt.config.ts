// https://nuxt.com/docs/api/configuration/nuxt-config
export default defineNuxtConfig({
  compatibilityDate: '2026-10-01',
  devtools: { enabled: true },

  modules: ['@nuxt/ui'],
  css: ['~/assets/css/main.css'],

  runtimeConfig: {
    // Server-only (never sent to the browser). Override with NUXT_API_BASE.
    // The browser talks only to this Nuxt server (/api/**); Nuxt forwards to the Go API.
    apiBase: 'http://127.0.0.1:8080/api/v1',
    public: {
      // Where LINE can reach the Go API (shown as the webhook URL to paste into the LINE console).
      // Override with NUXT_PUBLIC_WEBHOOK_BASE_URL, e.g. an ngrok URL during development.
      webhookBaseUrl: 'http://localhost:8080',
    },
  },

  app: {
    head: {
      htmlAttrs: { lang: 'th' },
      title: 'MSCS ร้านค้า',
    },
  },

  typescript: {
    strict: true,
  },
})
