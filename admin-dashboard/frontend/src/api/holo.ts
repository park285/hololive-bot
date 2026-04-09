import apiClient from './client'

export interface HoloApiResponse<T = unknown> {
  status: string
  message?: string
  error?: string
  data?: T
}

const holoPath = (path: string) => `/holo${path.startsWith('/') ? path : `/${path}`}`

export const holoApi = {
  get: async <T>(path: string, config?: Parameters<typeof apiClient.get<T>>[1]) => {
    const response = await apiClient.get<T>(holoPath(path), config)
    return response.data
  },

  post: async <T>(path: string, body?: unknown, config?: Parameters<typeof apiClient.post<T>>[2]) => {
    const response = await apiClient.post<T>(holoPath(path), body, config)
    return response.data
  },

  patch: async <T>(path: string, body?: unknown, config?: Parameters<typeof apiClient.patch<T>>[2]) => {
    const response = await apiClient.patch<T>(holoPath(path), body, config)
    return response.data
  },

  delete: async <T>(path: string, config?: Parameters<typeof apiClient.delete<T>>[1]) => {
    const response = await apiClient.delete<T>(holoPath(path), config)
    return response.data
  },
}
