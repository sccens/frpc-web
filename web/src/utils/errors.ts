export function errorMessage(err: unknown, fallback = '操作失败') {
  if (typeof err === 'object' && err !== null && 'response' in err) {
    const response = (err as { response?: { data?: { error?: string; message?: string } } }).response
    return response?.data?.error || response?.data?.message || fallback
  }
  return err instanceof Error ? err.message : fallback
}
