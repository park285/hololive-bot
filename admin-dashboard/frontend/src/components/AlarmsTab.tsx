import { useMemo, useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { alarmsApi, namesApi } from '@/api/holo'
import { queryKeys } from '@/api/queryKeys'
import type { Alarm } from '@/types'
import EditNameModal from '@/components/EditNameModal'
import { ConfirmModal } from '@/components/ConfirmModal'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Card } from '@/components/ui/Card'
import { Input } from '@/components/ui/Input'
import { Label } from '@/components/ui/Label'
import { Search, Trash2, Edit2, ChevronDown, ChevronUp, Bell, MapPin, User } from 'lucide-react'

interface AlarmGroup {
  roomId: string
  roomName: string
  userId: string
  userName: string
  alarms: Alarm[]
}

const numberFormatter = new Intl.NumberFormat('ko-KR')

const AlarmsTab = () => {
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set())
  const [alarmToDelete, setAlarmToDelete] = useState<Alarm | null>(null)

  const [editModal, setEditModal] = useState<{
    type: 'room' | 'user'
    id: string
    currentName: string
  } | null>(null)

  const { data: response, isLoading } = useQuery({
    queryKey: queryKeys.alarms.all,
    queryFn: alarmsApi.getAll,
  })

  const deleteAlarmMutation = useMutation({
    mutationFn: alarmsApi.delete,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.alarms.all })
    },
  })

  const setNameMutation = useMutation({
    mutationFn: async ({ type, id, name }: { type: 'room' | 'user'; id: string; name: string }) => {
      if (type === 'room') {
        return namesApi.setRoomName(id, name)
      }
      return namesApi.setUserName(id, name)
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.alarms.all })
    },
  })

  // 유저별로 그룹화
  const groupedAlarms = useMemo(() => {
    const alarms = response?.alarms || []
    const groups = new Map<string, AlarmGroup>()

    alarms.forEach((alarm: Alarm) => {
      const key = `${alarm.roomId}:${alarm.userId}`
      if (!groups.has(key)) {
        groups.set(key, {
          roomId: alarm.roomId,
          roomName: alarm.roomName,
          userId: alarm.userId,
          userName: alarm.userName,
          alarms: [],
        })
      }
      const group = groups.get(key)
      if (group) {
        group.alarms.push(alarm)
      }
    })

    // 한글 이름 기준 정렬 (방 이름 → 유저 이름)
    return Array.from(groups.values()).sort((a, b) => {
      if (a.roomName !== b.roomName) return a.roomName.localeCompare(b.roomName, 'ko')
      return a.userName.localeCompare(b.userName, 'ko')
    })
  }, [response])

  // 검색 필터링
  const filteredGroups = useMemo(() => {
    if (!search.trim()) return groupedAlarms

    const searchLower = search.toLowerCase()
    return groupedAlarms.filter(group =>
      group.roomName.toLowerCase().includes(searchLower) ||
      group.userName.toLowerCase().includes(searchLower) ||
      group.alarms.some(a => a.memberName.toLowerCase().includes(searchLower))
    )
  }, [groupedAlarms, search])

  const toggleGroup = (key: string) => {
    const newExpanded = new Set(expandedGroups)
    if (newExpanded.has(key)) {
      newExpanded.delete(key)
    } else {
      newExpanded.add(key)
    }
    setExpandedGroups(newExpanded)
  }

  const totalAlarms = filteredGroups.reduce((sum, g) => sum + g.alarms.length, 0)

  const handleDelete = (alarm: Alarm) => {
    setAlarmToDelete(alarm)
  }

  const confirmDelete = () => {
    if (!alarmToDelete) return
    void deleteAlarmMutation.mutateAsync({
      roomId: alarmToDelete.roomId,
      userId: alarmToDelete.userId,
      channelId: alarmToDelete.channelId,
    })
    setAlarmToDelete(null)
  }

  const handleEditName = (type: 'room' | 'user', id: string, currentName: string) => {
    setEditModal({ type, id, currentName })
  }

  const handleSaveName = (newName: string) => {
    if (!editModal) return
    void setNameMutation.mutateAsync({
      type: editModal.type,
      id: editModal.id,
      name: newName,
    })
  }

  if (isLoading) {
    return (
      <div className="text-center py-24 text-slate-500" aria-busy="true" aria-label="알람 데이터를 불러오는 중입니다…">
        <div className="animate-spin inline-block w-8 h-8 border-4 border-sky-200 border-t-sky-500 rounded-full mb-4" />
        <p>로딩 중…</p>
      </div>
    )
  }

  if (groupedAlarms.length === 0) {
    return (
      <div className="text-center py-12 bg-white rounded-2xl border border-slate-100 shadow-sm">
        <Bell className="mx-auto h-12 w-12 text-slate-200 mb-4" aria-hidden="true" />
        <p className="text-slate-500 font-medium">등록된 알람이 없습니다</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* 검색 바 */}
      <Card className="p-4 bg-white shadow-sm border-slate-200">
        <div className="flex flex-col md:flex-row items-center gap-4">
          <div className="relative w-full md:w-96">
            <Label htmlFor="alarm-search" className="sr-only">알람 검색</Label>
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={18} aria-hidden="true" />
            <Input
              id="alarm-search"
              placeholder="방 이름, 유저 이름, 멤버 이름…"
              value={search}
              onChange={(e) => { setSearch(e.target.value); }}
              className="pl-10 focus-visible:ring-2 focus-visible:ring-sky-200"
            />
          </div>
          <div className="text-sm text-slate-500 font-medium tabular-nums">
            총 <span className="text-slate-900 font-bold">{numberFormatter.format(filteredGroups.length)}</span>개 그룹 / <span className="text-slate-900 font-bold">{numberFormatter.format(totalAlarms)}</span>개 알람
          </div>
        </div>
      </Card>

      {/* 알람 그룹 목록 */}
      <div className="space-y-4" role="list">
        {filteredGroups.length === 0 ? (
          <div className="text-center py-12 bg-slate-50 rounded-xl border border-dashed border-slate-200">
            <Bell className="mx-auto h-12 w-12 text-slate-300 mb-3" aria-hidden="true" />
            <h3 className="text-lg font-medium text-slate-900">알람이 없습니다</h3>
            <p className="text-slate-500">새로운 알람을 등록하거나 검색어를 변경해보세요.</p>
          </div>
        ) : (
          filteredGroups.map((group) => {
            const groupKey = `${group.roomId}:${group.userId}`
            const isExpanded = expandedGroups.has(groupKey)
            const displayAlarms = isExpanded ? group.alarms : group.alarms.slice(0, 5)
            const hasMore = group.alarms.length > 5

            return (
              <div key={groupKey} role="listitem" className="bg-white border border-slate-200 rounded-xl overflow-hidden shadow-sm transition-all hover:shadow-md focus-within:ring-2 focus-within:ring-sky-100">
                {/* 그룹 헤더 */}
                <div
                  role="button"
                  tabIndex={0}
                  aria-expanded={isExpanded}
                  onClick={() => { toggleGroup(groupKey) }}
                  onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') toggleGroup(groupKey) }}
                  className="bg-slate-50/50 px-5 py-4 cursor-pointer hover:bg-slate-100/50 transition-colors border-b border-slate-100 outline-none focus-visible:bg-slate-100"
                >
                  <div className="flex items-center justify-between">
                    <div className="space-y-1">
                      <div className="flex items-center gap-2 flex-wrap">
                        <Badge variant="outline" className="bg-blue-50 text-blue-700 border-blue-200 gap-1 pr-3">
                          <MapPin size={12} aria-hidden="true" /> {group.roomName}
                        </Badge>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-6 w-6 text-slate-400 hover:text-blue-600 focus-visible:ring-2 focus-visible:ring-blue-200"
                          onClick={(e) => {
                            e.stopPropagation()
                            handleEditName('room', group.roomId, group.roomName)
                          }}
                          aria-label={`${group.roomName} 방 이름 수정`}
                        >
                          <Edit2 size={12} aria-hidden="true" />
                        </Button>

                        <span className="text-slate-300" aria-hidden="true">|</span>

                        <Badge variant="outline" className="bg-indigo-50 text-indigo-700 border-indigo-200 gap-1 pr-3">
                          <User size={12} aria-hidden="true" /> {group.userName}
                        </Badge>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-6 w-6 text-slate-400 hover:text-indigo-600 focus-visible:ring-2 focus-visible:ring-indigo-200"
                          onClick={(e) => {
                            e.stopPropagation()
                            handleEditName('user', group.userId, group.userName)
                          }}
                          aria-label={`${group.userName} 유저 이름 수정`}
                        >
                          <Edit2 size={12} aria-hidden="true" />
                        </Button>
                      </div>
                    </div>

                    <div className="flex items-center gap-4">
                      <span className="text-xs font-semibold text-slate-500 bg-slate-100 px-2 py-1 rounded-md tabular-nums">
                        {numberFormatter.format(group.alarms.length)}개
                      </span>
                      {isExpanded ? <ChevronUp className="text-slate-400" size={20} aria-hidden="true" /> : <ChevronDown className="text-slate-400" size={20} aria-hidden="true" />}
                    </div>
                  </div>
                </div>

                {/* 알람 목록 */}
                <div className="divide-y divide-slate-100" role="list">
                  {displayAlarms.map((alarm: Alarm, index: number) => (
                    <div key={`${alarm.channelId}-${String(index)}`} role="listitem" className="px-5 py-3 hover:bg-slate-50 flex items-center justify-between group transition-colors">
                      <div className="flex items-center gap-3">
                        <div className="h-8 w-8 rounded-full bg-slate-100 flex items-center justify-center text-slate-500 font-bold text-xs ring-2 ring-white" aria-hidden="true">
                          {alarm.memberName ? alarm.memberName[0] : '?'}
                        </div>
                        <div>
                          <div className="font-semibold text-slate-700 text-sm">
                            {alarm.memberName || '이름 없음'}
                          </div>
                          <div className="text-xs text-slate-400 font-mono">
                            {alarm.channelId}
                          </div>
                        </div>
                      </div>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={(e) => {
                          e.stopPropagation()
                          handleDelete(alarm)
                        }}
                        disabled={deleteAlarmMutation.isPending}
                        className="text-red-500 hover:text-red-600 hover:bg-red-50 opacity-0 group-hover:opacity-100 focus-visible:opacity-100 transition-all focus-visible:ring-2 focus-visible:ring-red-200"
                        aria-label={`${alarm.memberName || '알 수 없는 멤버'} 알람 삭제`}
                      >
                        <Trash2 size={16} aria-hidden="true" />
                      </Button>
                    </div>
                  ))}
                </div>

                {/* 더보기 버튼 */}
                {!isExpanded && hasMore && (
                  <div className="bg-slate-50/30 px-4 py-2 text-center border-t border-slate-100">
                    <button
                      onClick={(e) => {
                        e.stopPropagation()
                        toggleGroup(groupKey)
                      }}
                      className="text-xs font-medium text-slate-500 hover:text-slate-700 transition-colors focus-visible:underline outline-none"
                    >
                      +{numberFormatter.format(group.alarms.length - displayAlarms.length)}개 더보기
                    </button>
                  </div>
                )}
              </div>
            )
          })
        )}
      </div>

      {/* 이름 편집 모달 */}
      <EditNameModal
        isOpen={editModal !== null}
        onClose={() => { setEditModal(null) }}
        type={editModal?.type || 'room'}
        id={editModal?.id || ''}
        currentName={editModal?.currentName || ''}
        onSave={handleSaveName}
      />

      {/* 삭제 확인 모달 */}
      <ConfirmModal
        isOpen={alarmToDelete !== null}
        onClose={() => { setAlarmToDelete(null); }}
        onConfirm={confirmDelete}
        title="알람 삭제"
        message={
          alarmToDelete
            ? `다음 멤버의 알람 설정을 삭제하시겠습니까?`
            : ''
        }
        confirmText="삭제"
        confirmColor="danger"
      >
        {alarmToDelete && (
          <div className="bg-slate-50 p-4 rounded-lg mt-2 border border-slate-100 flex flex-col gap-2">
            <div className="flex justify-between items-center text-sm">
              <span className="text-slate-500">멤버</span>
              <span className="font-bold text-slate-800">{alarmToDelete.memberName || '이름 없음'}</span>
            </div>
            <div className="flex justify-between items-center text-sm">
              <span className="text-slate-500">채널 ID</span>
              <span className="font-mono text-slate-600 text-xs">{alarmToDelete.channelId}</span>
            </div>
          </div>
        )}
      </ConfirmModal>

    </div>
  )
}

export default AlarmsTab
