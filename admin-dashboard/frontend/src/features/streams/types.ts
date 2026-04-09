export interface Stream {
  id: string
  title: string
  status: string
  channel_name?: string
  channel_id: string
  link?: string
  thumbnail?: string
  start_scheduled?: string
  start_actual?: string
}

export type StreamOrg = 'hololive' | 'vspo' | 'stellive' | 'independents' | 'all'

export interface StreamsResponse {
  status: string
  org?: string
  streams: Stream[]
}
