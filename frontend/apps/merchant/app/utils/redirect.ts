/**
 * The page to go to after logging in, from ?redirect=. Only paths on this site: "/x" yes,
 * "//evil.example" or "https://evil.example" no (an "open redirect" would let a phishing link
 * that looks like ours send people to another site after they log in).
 */
export function safeRedirect(value: unknown): string {
  return typeof value === 'string' && value.startsWith('/') && !value.startsWith('//') && !value.startsWith('/\\')
    ? value
    : '/'
}
