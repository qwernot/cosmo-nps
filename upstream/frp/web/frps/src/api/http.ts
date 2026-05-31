// http.ts - Base HTTP client

export class HTTPError extends Error {
  status: number
  statusText: string

  constructor(status: number, statusText: string, message?: string) {
    super(message || statusText)
    this.status = status
    this.statusText = statusText
  }
}

const authKey = 'frps.basicAuth'

export function setBasicAuth(user: string, password: string) {
  const raw = `${user}:${password}`
  localStorage.setItem(authKey, btoa(raw))
}

export function clearBasicAuth() {
  localStorage.removeItem(authKey)
}

function authHeaders(): Record<string, string> {
  const token = localStorage.getItem(authKey)
  if (!token) {
    return {}
  }
  return { Authorization: `Basic ${token}` }
}

async function request<T>(url: string, options: RequestInit = {}): Promise<T> {
  const defaultOptions: RequestInit = {
    credentials: 'include',
    headers: authHeaders(),
  }

  const response = await fetch(url, {
    ...defaultOptions,
    ...options,
    headers: { ...defaultOptions.headers, ...options.headers },
  })

  if (!response.ok) {
    throw new HTTPError(
      response.status,
      response.statusText,
      `HTTP ${response.status}`,
    )
  }

  // Handle empty response (e.g. 204 No Content)
  if (response.status === 204) {
    return {} as T
  }

  return response.json()
}

export const http = {
  get: <T>(url: string, options?: RequestInit) =>
    request<T>(url, { ...options, method: 'GET' }),
  post: <T>(url: string, body?: any, options?: RequestInit) =>
    request<T>(url, {
      ...options,
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...options?.headers },
      body: JSON.stringify(body),
    }),
  put: <T>(url: string, body?: any, options?: RequestInit) =>
    request<T>(url, {
      ...options,
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', ...options?.headers },
      body: JSON.stringify(body),
    }),
  delete: <T>(url: string, options?: RequestInit) =>
    request<T>(url, { ...options, method: 'DELETE' }),
}
