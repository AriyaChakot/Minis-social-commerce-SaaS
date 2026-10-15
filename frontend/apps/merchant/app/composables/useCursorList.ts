import type { Page } from '@mscs/shared'

/**
 * A list backed by the API's keyset pagination ({items, next_cursor}).
 *
 * - The first page loads with useAsyncData: rendered on the server, then reused in the browser.
 * - Whenever `query` changes (search box, status filter), the list restarts from page one.
 * - loadMore() appends the next page in the browser ("โหลดเพิ่ม" button).
 *
 * Everything the browser needs after hydration is derived from `data` (which Nuxt transfers
 * from the server), not kept in plain refs filled during SSR, which would be lost.
 */
export function useCursorList<T>(key: MaybeRefOrGetter<string>, url: MaybeRefOrGetter<string>, query: MaybeRefOrGetter<Record<string, string | number | undefined>>) {
  const requestFetch = useRequestFetch()

  const { data, status, error, refresh } = useAsyncData(
    key,
    () => requestFetch<Page<T>>(toValue(url), { query: toValue(query) }),
    { watch: [() => toValue(query)] },
  )

  const more = ref<T[]>([]) as Ref<T[]>
  const moreCursor = ref<string | null | undefined>(undefined) // undefined = no extra page loaded yet
  const loadingMore = ref(false)

  // A new first page (refresh or new filter) discards the pages appended after it.
  watch(data, () => {
    more.value = []
    moreCursor.value = undefined
  })

  const items = computed<T[]>(() => [...(data.value?.items ?? []), ...more.value])
  const cursor = computed(() => (moreCursor.value === undefined ? data.value?.next_cursor ?? null : moreCursor.value))

  async function loadMore() {
    if (!cursor.value || loadingMore.value) return
    loadingMore.value = true
    try {
      const page = await $fetch<Page<T>>(toValue(url), { query: { ...toValue(query), cursor: cursor.value } })
      more.value.push(...page.items)
      moreCursor.value = page.next_cursor
    }
    finally {
      loadingMore.value = false
    }
  }

  return {
    items,
    hasMore: computed(() => cursor.value !== null),
    loading: computed(() => status.value === 'pending'),
    loadingMore,
    error,
    loadMore,
    refresh,
  }
}
