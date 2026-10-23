// Runs before every page navigation (".global" = no need to list it on each page).
// Logged-out visitors may only see the public pages; logged-in users skip them.
const PUBLIC_PAGES = ['/login', '/register']
// Open to everyone, logged in or not: an invitation link must work in both cases.
const OPEN_PREFIXES = ['/invite/']

export default defineNuxtRouteMiddleware(async (to) => {
  const { user, loaded, refresh } = useAuth()
  if (!loaded.value) {
    await refresh() // first navigation (usually on the server): ask the API who we are
  }

  if (OPEN_PREFIXES.some(p => to.path.startsWith(p))) {
    return
  }
  const isPublic = PUBLIC_PAGES.includes(to.path)
  if (!user.value && !isPublic) {
    // Remember the page, to come back to it after logging in.
    return navigateTo(to.fullPath === '/' ? '/login' : { path: '/login', query: { redirect: to.fullPath } })
  }
  if (user.value && isPublic) {
    return navigateTo('/')
  }
})
