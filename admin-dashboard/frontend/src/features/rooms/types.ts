export type ACLMode = 'whitelist' | 'blacklist'

export interface RoomsResponse {
  status: string
  rooms: string[]
  aclEnabled: boolean
  aclMode: ACLMode
}

export interface AddRoomRequest {
  room: string
}

export interface RemoveRoomRequest {
  room: string
}
