// Catch-all proxy: /api/<anything> -> Go API /api/v1/<anything>, with the session token added.
// Routes with their own file (e.g. /api/auth/login) take precedence over this one.
//
// The browser therefore needs no CORS (same origin) and never handles the token.
export default defineEventHandler(async (event) => {
  const path = getRouterParam(event, 'path') ?? ''
  const method = event.method
  const hasBody = !['GET', 'HEAD', 'DELETE'].includes(method)

  const result = await callApi(event, `/${path}`, {
    method,
    query: getQuery(event),
    // Raw text: forwarded byte for byte, so the Go API sees exactly what the browser sent.
    body: hasBody ? ((await readRawBody(event, 'utf8')) ?? '') : undefined,
    contentType: getRequestHeader(event, 'content-type'),
  })
  return relay(event, result)
})
