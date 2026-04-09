import { useDeferredValue, useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import Bell from 'lucide-react/dist/esm/icons/bell'
import { queryKeys } from '@/api/queryKeys'
import { ConfirmModal } from '@/components/ConfirmModal'
import EditNameModal from '@/components/EditNameModal'
import { alarmsApi, namesApi } from '@/features/alarms/api'
import { AlarmGroups, type AlarmGroup } from '@/features/alarms/components/AlarmGroups'
import { AlarmsToolbar } from '@/features/alarms/components/AlarmsToolbar'
import type { Alarm } from '@/features/alarms/types'

const ALARM_GROUP_PAGE_SIZE = 20

export const AlarmsPage = () => {
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const deferredSearch = useDeferredValue(search)
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set())
  const [alarmToDelete, setAlarmToDelete] = useState<Alarm | null>(null)
  const [visibleGroupCount, setVisibleGroupCount] = useState(ALARM_GROUP_PAGE_SIZE)
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

  useEffect(() => {
    setVisibleGroupCount(ALARM_GROUP_PAGE_SIZE)
  }, [deferredSearch])

  const groupedAlarms = useMemo(() => {
    const alarms = response?.alarms || []
    const groups = new Map<string, AlarmGroup>()

    alarms.forEach((alarm) => {
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

    return Array.from(groups.values()).sort((first, second) => {
      if (first.roomName !== second.roomName) {
        return first.roomName.localeCompare(second.roomName, 'ko')
      }
      return first.userName.localeCompare(second.userName, 'ko')
    })
  }, [response])

  const filteredGroups = useMemo(() => {
    if (!deferredSearch.trim()) return groupedAlarms

    const searchLower = deferredSearch.toLowerCase()
    return groupedAlarms.filter((group) =>
      group.roomName.toLowerCase().includes(searchLower) ||
      group.userName.toLowerCase().includes(searchLower) ||
      group.alarms.some((alarm) => alarm.memberName.toLowerCase().includes(searchLower)),
    )
  }, [deferredSearch, groupedAlarms])

  const totalAlarms = filteredGroups.reduce((sum, group) => sum + group.alarms.length, 0)

  const toggleGroup = (groupKey: string) => {
    const nextExpandedGroups = new Set(expandedGroups)
    if (nextExpandedGroups.has(groupKey)) {
      nextExpandedGroups.delete(groupKey)
    } else {
      nextExpandedGroups.add(groupKey)
    }
    setExpandedGroups(nextExpandedGroups)
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
      <AlarmsToolbar
        search={search}
        onSearchChange={setSearch}
        groupCount={filteredGroups.length}
        alarmCount={totalAlarms}
      />

      <AlarmGroups
        groups={filteredGroups}
        expandedGroups={expandedGroups}
        onToggleGroup={toggleGroup}
        onDeleteAlarm={setAlarmToDelete}
        onEditName={(type, id, currentName) => {
          setEditModal({ type, id, currentName })
        }}
        visibleGroupCount={visibleGroupCount}
        onLoadMore={() => { setVisibleGroupCount((prev) => prev + ALARM_GROUP_PAGE_SIZE) }}
        isDeleting={deleteAlarmMutation.isPending}
      />

      <EditNameModal
        isOpen={editModal !== null}
        onClose={() => { setEditModal(null) }}
        type={editModal?.type || 'room'}
        id={editModal?.id || ''}
        currentName={editModal?.currentName || ''}
        onSave={handleSaveName}
      />

      <ConfirmModal
        isOpen={alarmToDelete !== null}
        onClose={() => { setAlarmToDelete(null) }}
        onConfirm={confirmDelete}
        title="알람 삭제"
        message={alarmToDelete ? '다음 멤버의 알람 설정을 삭제하시겠습니까?' : ''}
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
