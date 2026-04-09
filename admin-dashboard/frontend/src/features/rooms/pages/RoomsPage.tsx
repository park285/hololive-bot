import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { queryKeys } from '@/api/queryKeys'
import { ConfirmModal } from '@/components/ConfirmModal'
import { Button } from '@/components/ui/Button'
import { RoomsAclSection } from '@/features/rooms/components/RoomsAclSection'
import { RoomsListSection } from '@/features/rooms/components/RoomsListSection'
import { roomsApi } from '@/features/rooms/api'
import type { ACLMode } from '@/features/rooms/types'

const MODE_LABELS: Record<ACLMode, { title: string; listTitle: string; emptyText: string; addTitle: string; deleteConfirm: string; description: string; indicator: string }> = {
  whitelist: {
    title: '화이트리스트',
    listTitle: '허용된 채팅방 목록',
    emptyText: '허용된 방이 없습니다.',
    addTitle: '허용 방 추가',
    deleteConfirm: '정말 이 채팅방을 허용 목록에서 삭제하시겠습니까?',
    description: '화이트리스트 모드입니다. 등록된 채팅방에서만 봇이 작동합니다.',
    indicator: 'bg-emerald-400',
  },
  blacklist: {
    title: '블랙리스트',
    listTitle: '차단된 채팅방 목록',
    emptyText: '차단된 방이 없습니다.',
    addTitle: '차단 방 추가',
    deleteConfirm: '정말 이 채팅방을 차단 목록에서 삭제하시겠습니까?',
    description: '블랙리스트 모드입니다. 등록된 채팅방에서는 봇이 작동하지 않습니다.',
    indicator: 'bg-rose-400',
  },
}

export const RoomsPage = () => {
  const queryClient = useQueryClient()
  const [newRoom, setNewRoom] = useState('')
  const [deleteModal, setDeleteModal] = useState<{ isOpen: boolean; room: string }>({ isOpen: false, room: '' })

  const { data: response, isLoading, isError, error, refetch } = useQuery({
    queryKey: queryKeys.rooms.all,
    queryFn: roomsApi.getAll,
  })

  const addRoomMutation = useMutation({
    mutationFn: roomsApi.add,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.rooms.all })
      setNewRoom('')
    },
  })

  const removeRoomMutation = useMutation({
    mutationFn: roomsApi.remove,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.rooms.all })
    },
  })

  const setACLMutation = useMutation({
    mutationFn: roomsApi.setACL,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.rooms.all })
    },
  })

  const rooms = response?.rooms || []
  const aclEnabled = response?.aclEnabled ?? true
  const aclMode: ACLMode = response?.aclMode ?? 'blacklist'
  const labels = MODE_LABELS[aclMode]
  const isBlacklist = aclMode === 'blacklist'

  const handleAddRoom = () => {
    const room = newRoom.trim()
    if (!room) return
    void addRoomMutation.mutateAsync({ room })
  }

  const confirmDelete = () => {
    if (deleteModal.room) {
      void removeRoomMutation.mutateAsync({ room: deleteModal.room })
    }
    setDeleteModal({ isOpen: false, room: '' })
  }

  const handleToggleACL = () => {
    void setACLMutation.mutateAsync({ enabled: !aclEnabled })
  }

  const handleModeChange = (mode: ACLMode) => {
    if (mode === aclMode) return
    void setACLMutation.mutateAsync({ mode })
  }

  if (isLoading) {
    return (
      <div className="text-center py-24 text-slate-500" aria-busy="true" aria-label="데이터를 불러오는 중입니다…">
        <div className="animate-spin inline-block w-8 h-8 border-4 border-sky-200 border-t-sky-500 rounded-full mb-4" />
        <p>데이터를 불러오는 중입니다…</p>
      </div>
    )
  }

  if (isError) {
    return (
      <div role="alert" className="text-center py-12 bg-rose-50 rounded-2xl border border-rose-100">
        <div className="text-rose-600 font-bold mb-2">채팅방 목록을 불러올 수 없습니다</div>
        <div className="text-xs text-rose-500 mb-4">
          {error instanceof Error ? error.message : '알 수 없는 오류가 발생했습니다'}
        </div>
        <Button
          onClick={() => { void refetch() }}
          className="bg-rose-600 hover:bg-rose-700 text-white focus-visible:ring-2 focus-visible:ring-rose-200"
          aria-label="데이터 다시 불러오기"
        >
          다시 시도
        </Button>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <RoomsAclSection
        aclEnabled={aclEnabled}
        aclMode={aclMode}
        description={aclEnabled ? labels.description : '접근 제어가 비활성화되었습니다. 모든 채팅방에서 봇이 명령을 수행합니다.'}
        isPending={setACLMutation.isPending}
        onToggleACL={handleToggleACL}
        onModeChange={handleModeChange}
      />

      <RoomsListSection
        rooms={rooms}
        listTitle={labels.listTitle}
        emptyText={labels.emptyText}
        addTitle={labels.addTitle}
        indicatorClassName={labels.indicator}
        isBlacklist={isBlacklist}
        infoMessage={
          isBlacklist
            ? '차단 목록에 추가된 채팅방에서는 봇이 명령에 응답하지 않습니다.'
            : '오픈프로필 채팅방의 경우, 봇이 방에 입장해 있어야 ID를 확인할 수 있습니다.'
        }
        newRoom={newRoom}
        onNewRoomChange={setNewRoom}
        onAddRoom={handleAddRoom}
        onDeleteRoom={(room) => { setDeleteModal({ isOpen: true, room }) }}
        addPending={addRoomMutation.isPending}
        removePending={removeRoomMutation.isPending}
      />

      <ConfirmModal
        isOpen={deleteModal.isOpen}
        onClose={() => { setDeleteModal({ isOpen: false, room: '' }) }}
        onConfirm={confirmDelete}
        title={isBlacklist ? '차단 해제' : '채팅방 삭제'}
        message={labels.deleteConfirm}
        confirmText="삭제"
        confirmColor="danger"
      >
        {deleteModal.room && (
          <div className="bg-slate-50 p-3 rounded-lg mt-2 text-center font-mono font-bold text-slate-800 border border-slate-200">
            {deleteModal.room}
          </div>
        )}
      </ConfirmModal>
    </div>
  )
}
