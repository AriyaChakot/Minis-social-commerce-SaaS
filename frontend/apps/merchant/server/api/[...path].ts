// Catch-all proxy: /api/<anything> -> Go API /api/v1/<anything>, with the session token added.
// Routes with their own file (e.g. /api/auth/login) take precedence over this one.
//
// The browser therefore needs no CORS (same origin) and never handles the token.
export default defineEventHandler(async (event) => {
  const path = getRouterParam(event, 'path') ?? ''
  const method = event.method
  const hasBody = !['GET', 'HEAD', 'DELETE'].includes(method)
  const contentType = getRequestHeader(event, 'content-type')
  // Forwarded byte for byte, so the Go API sees exactly what the browser sent. File uploads
  // (multipart) are binary: read as bytes, since decoding them as UTF-8 text would corrupt them.
  const binary = contentType?.startsWith('multipart/') ?? false
  const raw = hasBody ? await readRawBody(event, binary ? false : 'utf8') : undefined

  const result = await callApi(event, `/${path}`, {
    method,
    query: getQuery(event),
    body: hasBody ? (raw ?? '') : undefined,
    contentType,
  })
  return relay(event, result)
})
