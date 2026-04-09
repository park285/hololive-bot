import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { queryKeys } from '@/api/queryKeys'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Card } from '@/components/ui/Card'
import { Input } from '@/components/ui/Input'
import { Label } from '@/components/ui/Label'
import { ConfirmModal } from '@/components/ConfirmModal'
import { roomsApi } from '@/features/rooms/api'
import type { ACLMode } from '@/features/rooms/types'
import Plus from 'lucide-react/dist/esm/icons/plus'
import Trash2 from 'lucide-react/dist/esm/icons/trash-2'
import Shield from 'lucide-react/dist/esm/icons/shield'
import ShieldAlert from 'lucide-react/dist/esm/icons/shield-alert'
import ShieldBan from 'lucide-react/dist/esm/icons/shield-ban'
import Info from 'lucide-react/dist/esm/icons/info'
import clsx from 'clsx'

const numberFormatter = new Intl.NumberFormat('ko-KR')

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

const RoomsTab = () => {
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

  const handleAddRoom = () => {
    const room = newRoom.trim()
    if (!room) return
    void addRoomMutation.mutateAsync({ room })
  }

  const handleDeleteClick = (room: string) => {
    setDeleteModal({ isOpen: true, room })
  }

  const confirmDelete = () => {
    if (deleteModal.room) {
      void removeRoomMutation.mutateAsync({ room: deleteModal.room })
    }
    setDeleteModal({ isOpen: false, room: '' })
  }

  const handleToggleACL = () => {
    void setACLMutation.mutateAsync({ enabled: !response?.aclEnabled })
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

  const rooms = response?.rooms || []
  const aclEnabled = response?.aclEnabled ?? true
  const aclMode: ACLMode = response?.aclMode ?? 'blacklist'
  const labels = MODE_LABELS[aclMode]
  const isBlacklist = aclMode === 'blacklist'

  return (
    <div className="space-y-6">
      {/* ACL 설정 섹션 */}
      <Card className={clsx("transition-all duration-300 border", aclEnabled ? "bg-white border-blue-100 shadow-sm" : "bg-slate-50 border-slate-200")}>
        <div className="p-6 space-y-5">
          {/* ACL 활성/비활성 토글 */}
          <div className="flex flex-col md:flex-row items-center justify-between gap-4">
            <div className="flex items-start gap-4">
              <div className={clsx("p-3 rounded-full mt-1 transition-colors", aclEnabled ? (isBlacklist ? "bg-rose-50" : "bg-blue-50") : "bg-slate-200")} aria-hidden="true">
                {!aclEnabled ? (
                  <ShieldAlert className="text-slate-500" size={24} />
                ) : isBlacklist ? (
                  <ShieldBan className="text-rose-600" size={24} />
                ) : (
                  <Shield className="text-blue-600" size={24} />
                )}
              </div>
              <div>
                <h3 className="text-lg font-display font-bold text-slate-900 mb-1">방 접근 제어 (ACL)</h3>
                <p className="text-sm text-slate-500 max-w-lg leading-relaxed">
                  {aclEnabled ? labels.description : '접근 제어가 비활성화되었습니다. 모든 채팅방에서 봇이 명령을 수행합니다.'}
                </p>
              </div>
            </div>

            <div className="flex items-center gap-3">
              <span className={clsx("text-sm font-bold", aclEnabled ? "text-blue-600" : "text-slate-500")}>
                {aclEnabled ? "활성화됨" : "비활성화됨"}
              </span>
              <button
                onClick={handleToggleACL}
                disabled={setACLMutation.isPending}
                role="switch"
                aria-checked={aclEnabled}
                aria-label="방 접근 제어 토글"
                className={clsx(
                  "relative inline-flex h-7 w-12 items-center rounded-full transition-colors focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500",
                  aclEnabled ? "bg-blue-600" : "bg-slate-300",
                  setACLMutation.isPending && "opacity-50 cursor-wait"
                )}
              >
                <span
                  className={clsx(
                    "inline-block h-5 w-5 transform rounded-full bg-white shadow transition-transform",
                    aclEnabled ? "translate-x-6" : "translate-x-1"
                  )}
                />
              </button>
            </div>
          </div>

          {/* 모드 전환 (ACL 활성화 시만 표시) */}
          {aclEnabled && (
            <div className="flex items-center gap-3 pt-3 border-t border-slate-100">
              <span className="text-sm font-semibold text-slate-600 mr-1">모드</span>
              <div className="inline-flex rounded-lg bg-slate-100 p-0.5" role="radiogroup" aria-label="ACL 모드 선택">
                <button
                  role="radio"
                  aria-checked={!isBlacklist}
                  onClick={() => { handleModeChange('whitelist') }}
                  disabled={setACLMutation.isPending}
                  className={clsx(
                    "px-4 py-1.5 rounded-md text-sm font-semibold transition-all",
                    !isBlacklist
                      ? "bg-white text-blue-700 shadow-sm"
                      : "text-slate-500 hover:text-slate-700",
                    setACLMutation.isPending && "opacity-50 cursor-wait"
                  )}
                >
                  화이트리스트
                </button>
                <button
                  role="radio"
                  aria-checked={isBlacklist}
                  onClick={() => { handleModeChange('blacklist') }}
                  disabled={setACLMutation.isPending}
                  className={clsx(
                    "px-4 py-1.5 rounded-md text-sm font-semibold transition-all",
                    isBlacklist
                      ? "bg-white text-rose-700 shadow-sm"
                      : "text-slate-500 hover:text-slate-700",
                    setACLMutation.isPending && "opacity-50 cursor-wait"
                  )}
                >
                  블랙리스트
                </button>
              </div>
            </div>
          )}
        </div>
      </Card>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <div className="lg:col-span-2 space-y-6">
          <div className="flex items-center justify-between">
            <h3 className="text-lg font-display font-bold text-slate-900">{labels.listTitle}</h3>
            <Badge variant="secondary" className="text-slate-600 tabular-nums">{numberFormatter.format(rooms.length)}개</Badge>
          </div>

          {/* 방 목록 */}
          <div className="bg-white rounded-xl border border-slate-200 shadow-sm divide-y divide-slate-100 overflow-hidden" role="list">
            {rooms.length === 0 ? (
              <div className="text-slate-400 text-center py-12 flex flex-col items-center gap-2">
                <Info size={32} className="opacity-20" aria-hidden="true" />
                <p>{labels.emptyText}</p>
              </div>
            ) : (
              rooms.map((room: string) => (
                <div key={room} role="listitem" className="flex items-center justify-between px-6 py-4 hover:bg-slate-50 transition-colors group focus-within:bg-slate-50">
                  <div className="flex items-center gap-3">
                    <div className={clsx("w-2 h-2 rounded-full", labels.indicator)} aria-hidden="true" />
                    <span className="font-mono text-slate-700 font-medium select-all">{room}</span>
                  </div>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => { handleDeleteClick(room); }}
                    disabled={removeRoomMutation.isPending}
                    className="text-slate-400 hover:text-red-600 hover:bg-red-50 opacity-0 group-hover:opacity-100 focus-visible:opacity-100 transition-all focus-visible:ring-2 focus-visible:ring-red-200"
                    aria-label={`${room} 방 삭제`}
                  >
                    <Trash2 size={16} aria-hidden="true" />
                  </Button>
                </div>
              ))
            )}
          </div>
        </div>

        <div>
          {/* 새 방 추가 */}
          <Card className="sticky top-6">
            <div className="p-5 space-y-4">
              <h3 className="font-display font-bold text-slate-900 flex items-center gap-2">
                <Plus className={isBlacklist ? "text-rose-500" : "text-blue-500"} size={18} aria-hidden="true" /> {labels.addTitle}
              </h3>

              <div className={clsx("p-3 rounded-lg flex items-start gap-2 border", isBlacklist ? "bg-rose-50 border-rose-100" : "bg-blue-50 border-blue-100")}>
                <Info className={clsx("shrink-0 mt-0.5", isBlacklist ? "text-rose-600" : "text-blue-600")} size={16} aria-hidden="true" />
                <p className={clsx("text-xs leading-snug", isBlacklist ? "text-rose-700" : "text-blue-700")}>
                  {isBlacklist
                    ? '차단 목록에 추가된 채팅방에서는 봇이 명령에 응답하지 않습니다.'
                    : '오픈프로필 채팅방의 경우, 봇이 방에 입장해 있어야 ID를 확인할 수 있습니다.'
                  }
                </p>
              </div>

              <div className="space-y-3">
                <div className="space-y-1.5">
                  <Label htmlFor="new-room-id" className="text-xs font-semibold text-slate-500">
                    채팅방 ID (RoomID)
                  </Label>
                  <Input
                    id="new-room-id"
                    value={newRoom}
                    onChange={(e) => { setNewRoom(e.target.value); }}
                    onKeyDown={(e) => { if (e.key === 'Enter') handleAddRoom(); }}
                    placeholder="예: 451788135895779"
                    className={clsx("font-mono focus-visible:ring-2", isBlacklist ? "focus-visible:ring-rose-200" : "focus-visible:ring-blue-200")}
                    disabled={addRoomMutation.isPending}
                  />
                </div>
                <Button
                  onClick={handleAddRoom}
                  disabled={addRoomMutation.isPending || !newRoom.trim()}
                  className={clsx("w-full shadow-sm", isBlacklist ? "bg-rose-600 hover:bg-rose-700 shadow-rose-200" : "bg-blue-600 hover:bg-blue-700 shadow-blue-200")}
                  aria-label="채팅방 추가하기"
                >
                  {addRoomMutation.isPending ? '추가 중…' : '추가하기'}
                </Button>
              </div>
            </div>
          </Card>
        </div>
      </div>

      <ConfirmModal
        isOpen={deleteModal.isOpen}
        onClose={() => { setDeleteModal({ isOpen: false, room: '' }); }}
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

export default RoomsTab
