/**
 * A copy of `source` that only updates after it stops changing for `delay` ms.
 * Used for search boxes: typing "เสื้อยืด" sends one request, not one per keystroke.
 */
export function useDebounced<T>(source: Ref<T>, delay = 300): Readonly<Ref<T>> {
  const debounced = ref(source.value) as Ref<T>
  let timer: ReturnType<typeof setTimeout> | undefined

  watch(source, (value) => {
    clearTimeout(timer)
    timer = setTimeout(() => { debounced.value = value }, delay)
  })
  onScopeDispose(() => clearTimeout(timer))

  return readonly(debounced) as Readonly<Ref<T>>
}
