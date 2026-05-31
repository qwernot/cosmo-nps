import { http } from './http'

export interface SessionInfo {
  name: string
  role: string
  isAdmin: boolean
}

export function getSession() {
  return http.get<SessionInfo>('/api/session')
}
