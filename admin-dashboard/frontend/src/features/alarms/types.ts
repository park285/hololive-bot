export interface Alarm {
  roomId: string
  roomName: string
  userId: string
  userName: string
  channelId: string
  memberName: string
}

export interface AlarmsResponse {
  status: string
  alarms: Alarm[]
}
