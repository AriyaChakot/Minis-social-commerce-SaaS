import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// The console is a static single-page app: no server of its own. In development Vite forwards
// /api to the Go API, so the browser sees one origin (http://localhost:5174) and the API's
// SameSite=Strict session cookie works. In production, put the built files and the API behind
// one domain the same way (e.g. a reverse proxy routing /api to the API).
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
})
