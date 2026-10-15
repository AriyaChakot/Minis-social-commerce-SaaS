// Runs before every page navigation (".global" = no need to list it on each page).
// Logged-out visitors may only see the public pages; logged-in users skip them.
const PUBLIC_PAGES = ['/login', '/register']

export default defineNuxtRouteMiddleware(async (to) => {
  const { user, loaded, refresh } = useAuth()
  if (!loaded.value) {
    await refresh() // first navigation (usually on the server): ask the API who we are
  }

  const isPublic = PUBLIC_PAGES.includes(to.path)
  if (!user.value && !isPublic) {
    return navigateTo('/login')
  }
  if (user.value && isPublic) {
    return navigateTo('/')
  }
})
