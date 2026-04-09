export interface Member {
  id: number
  channelId: string
  name: string
  aliases: {
    ko: string[]
    ja: string[]
  }
  nameJa?: string
  nameKo?: string
  isGraduated: boolean
}

export interface MembersResponse {
  status: string
  members: Member[]
}

export interface AddAliasRequest {
  type: 'ko' | 'ja'
  alias: string
}

export interface RemoveAliasRequest {
  type: 'ko' | 'ja'
  alias: string
}

export interface SetGraduationRequest {
  isGraduated: boolean
}

export interface UpdateChannelRequest {
  channelId: string
}
