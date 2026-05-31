import { http } from './http'

export interface PortRange {
  start?: number
  end?: number
  single?: number
}

export interface UserRecord {
  name: string
  password?: string
  token?: string
  role?: string
  enabled: boolean
  allowPorts?: PortRange[]
  maxPorts?: number
  hasPassword?: boolean
  hasToken?: boolean
  createdAt?: string
  updatedAt?: string
  lastLoginAt?: string
}

export function listUsers() {
  return http.get<UserRecord[]>('/api/users')
}

export function saveUser(user: UserRecord) {
  const method = user.name ? http.put : http.post
  const url = user.name ? `/api/users/${encodeURIComponent(user.name)}` : '/api/users'
  return method<UserRecord>(url, user)
}

export function deleteUser(name: string) {
  return http.delete(`/api/users/${encodeURIComponent(name)}`)
}
