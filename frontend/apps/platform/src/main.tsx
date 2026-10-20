import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { MutationCache, QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createBrowserRouter, Navigate, RouterProvider } from 'react-router'
import './index.css'
import { ApiError } from './api'
import { RequireAdmin } from './auth'
import { Layout } from './components/Layout'
import { LoginPage } from './pages/LoginPage'
import { OverviewPage } from './pages/OverviewPage'
import { TenantsPage } from './pages/TenantsPage'

// A 401 anywhere (the session expired, or was logged out elsewhere) refreshes "who am I", which
// becomes null, and the route guard sends the user to the login page.
const onError = (e: unknown) => {
  if (e instanceof ApiError && (e.status === 401 || e.status === 403)) {
    void queryClient.invalidateQueries({ queryKey: ['me'] })
  }
}
const queryClient = new QueryClient({
  queryCache: new QueryCache({ onError }),
  mutationCache: new MutationCache({ onError }),
  defaultOptions: {
    queries: { retry: (count, e) => !(e instanceof ApiError && e.status < 500) && count < 2 },
  },
})

const router = createBrowserRouter([
  { path: '/login', element: <LoginPage /> },
  {
    element: <RequireAdmin />,
    children: [{
      element: <Layout />,
      children: [
        { index: true, element: <OverviewPage /> },
        { path: 'tenants', element: <TenantsPage /> },
        { path: '*', element: <Navigate to="/" replace /> },
      ],
    }],
  },
])

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
)
