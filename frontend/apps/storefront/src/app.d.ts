// See https://svelte.dev/docs/kit/types#app.d.ts
declare global {
  namespace App {
    // The shape of `page.error` for error pages.
    interface Error {
      message: string
    }
  }
}

export {}
