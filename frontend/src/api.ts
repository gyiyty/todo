const csrf = () => document.cookie.split('; ').find(value => value.startsWith('todo_csrf='))?.split('=')[1] || ''

export class APIError extends Error { constructor(public status: number, message: string) { super(message) } }

export async function api<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers)
  if (options.body) headers.set('Content-Type', 'application/json')
  if (options.method && !['GET', 'HEAD'].includes(options.method)) headers.set('X-CSRF-Token', csrf())
  const response = await fetch(`/api/v1${path}`, { credentials: 'same-origin', ...options, headers })
  if (!response.ok) {
    const payload = await response.json().catch(() => ({ error: '请求失败' }))
    throw new APIError(response.status, payload.error || '请求失败')
  }
  if (response.status === 204) return undefined as T
  return response.json()
}

export const post = <T>(path: string, body?: unknown) => api<T>(path, { method: 'POST', body: body === undefined ? undefined : JSON.stringify(body) })
export const patch = <T>(path: string, body: unknown) => api<T>(path, { method: 'PATCH', body: JSON.stringify(body) })
export const put = <T>(path: string, body: unknown) => api<T>(path, { method: 'PUT', body: JSON.stringify(body) })
export const del = <T>(path: string) => api<T>(path, { method: 'DELETE' })
