/**
 * Calls fn every intervalMs while the tab is visible, and once right away when the user comes
 * back to the tab. Stops when the component unmounts. (Polling is the simple first step; a push
 * channel such as Server-Sent Events would replace it once there are many open inboxes.)
 */
export function usePolling(fn: () => unknown, intervalMs: number) {
  let timer: ReturnType<typeof setInterval> | undefined

  const tick = () => {
    if (document.visibilityState === 'visible') fn()
  }

  onMounted(() => {
    timer = setInterval(tick, intervalMs)
    document.addEventListener('visibilitychange', tick)
  })
  onBeforeUnmount(() => {
    clearInterval(timer)
    document.removeEventListener('visibilitychange', tick)
  })
}
