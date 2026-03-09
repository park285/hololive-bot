import { useCallback, useMemo, useOptimistic, useState, startTransition } from 'react'
import { ConfirmModal } from '@/components/ConfirmModal'
import ChannelEditModal from './ChannelEditModal'
import AddMemberModal from '@/components/AddMemberModal'
import EditNameModal from '@/components/EditNameModal'
import MemberCard from '@/components/MemberCard'
import { useQuery } from '@tanstack/react-query'
import { membersApi } from '@/api/holo'
import { queryKeys } from '@/api/queryKeys'
import type { Member, MembersResponse } from '@/types'
import { Button } from '@/components/ui/Button'
import { Label } from '@/components/ui/Label'
import Search from 'lucide-react/dist/esm/icons/search';
import Plus from 'lucide-react/dist/esm/icons/plus';
import { useSSRData } from '@/hooks/useSSRData'
import { useMemberMutations, optimisticMemberReducer } from '@/hooks/useMemberMutations'

const numberFormatter = new Intl.NumberFormat('ko-KR')

const MembersTab = () => {
  // SSR 데이터 소비 (useSSRData 훅 활용)
  const ssrInitialData = useSSRData('members', (data) =>
    data?.status === 'ok' && data.members ? (data as MembersResponse) : undefined
  )

  const { data: response, isLoading } = useQuery({
    queryKey: queryKeys.members.all,
    queryFn: membersApi.getAll,
    initialData: ssrInitialData,
  })

  // Mutations (useMemberMutations 훅으로 중앙화)
  const {
    addAlias: addAliasMutation,
    removeAlias: removeAliasMutation,
    updateChannel: updateChannelMutation,
    updateName: updateNameMutation,
    setGraduation: setGraduationMutation,
    addMember: addMemberMutation,
  } = useMemberMutations()

  // 상태 관리
  const [searchTerm, setSearchTerm] = useState('')
  const [hideGraduated, setHideGraduated] = useState<boolean>(() => {
    const saved = localStorage.getItem('hideGraduated')
    return saved !== null ? saved === 'true' : true
  })

  // 모달 상태
  type ModalState =
    | { type: 'none' }
    | { type: 'removeAlias'; memberId: number; aliasType: 'ko' | 'ja'; alias: string }
    | { type: 'graduation'; memberId: number; memberName: string; currentStatus: boolean }
    | { type: 'channelEdit'; memberId: number; memberName: string; currentChannelId: string }
    | { type: 'nameEdit'; memberId: number; currentName: string }

  const [modal, setModal] = useState<ModalState>({ type: 'none' })
  const [isAddModalOpen, setIsAddModalOpen] = useState(false)

  // Optimistic 업데이트 설정
  const allMembers = useMemo(
    () =>
      (response?.members ?? []).map((member: Member) => ({
        ...member,
        aliases: {
          ko: member.aliases.ko,
          ja: member.aliases.ja,
        },
      })),
    [response?.members],
  )

  // optimisticMemberReducer를 훅에서 import하여 사용
  const [optimisticMembers, setOptimisticMembers] = useOptimistic(
    allMembers,
    optimisticMemberReducer
  )

  const toggleHideGraduated = () => {
    const newValue = !hideGraduated
    setHideGraduated(newValue)
    localStorage.setItem('hideGraduated', String(newValue))
  }

  const handleAddAlias = useCallback((memberId: number, type: 'ko' | 'ja', rawAlias: string) => {
    const alias = rawAlias.trim()
    if (!alias) return

    setOptimisticMembers({ type: 'addAlias', memberId, aliasType: type, alias })
    void addAliasMutation.mutateAsync({ memberId, type, alias })
  }, [addAliasMutation, setOptimisticMembers])

  const handleRemoveAlias = useCallback((memberId: number, type: 'ko' | 'ja', alias: string) => {
    setModal({ type: 'removeAlias', memberId, aliasType: type, alias })
  }, [])

  const confirmRemoveAlias = useCallback(() => {
    if (modal.type !== 'removeAlias') return

    const payload = {
      memberId: modal.memberId,
      aliasType: modal.aliasType,
      alias: modal.alias,
    }

    setModal({ type: 'none' })

    startTransition(() => {
      setOptimisticMembers({
        type: 'removeAlias',
        memberId: payload.memberId,
        aliasType: payload.aliasType,
        alias: payload.alias,
      })
    })

    removeAliasMutation.mutate({
      memberId: payload.memberId,
      type: payload.aliasType,
      alias: payload.alias,
    })
  }, [modal, removeAliasMutation, setOptimisticMembers])

  const handleUpdateChannel = useCallback((memberId: number, memberName: string, currentChannelId: string) => {
    setModal({ type: 'channelEdit', memberId, memberName, currentChannelId })
  }, [])

  const confirmUpdateChannel = useCallback((newChannelId: string) => {
    if (modal.type !== 'channelEdit') return

    setOptimisticMembers({ type: 'updateChannel', memberId: modal.memberId, channelId: newChannelId })
    void updateChannelMutation.mutateAsync({ memberId: modal.memberId, channelId: newChannelId })
  }, [modal, setOptimisticMembers, updateChannelMutation])

  const handleEditName = useCallback((memberId: number, currentName: string) => {
    setModal({ type: 'nameEdit', memberId, currentName })
  }, [])

  const confirmEditName = useCallback((newName: string) => {
    if (modal.type !== 'nameEdit') return

    setOptimisticMembers({ type: 'updateName', memberId: modal.memberId, name: newName })
    void updateNameMutation.mutateAsync({ memberId: modal.memberId, name: newName })
  }, [modal, setOptimisticMembers, updateNameMutation])

  const handleToggleGraduation = useCallback((memberId: number, memberName: string, currentStatus: boolean) => {
    setModal({ type: 'graduation', memberId, memberName, currentStatus })
  }, [])

  const confirmToggleGraduation = useCallback(() => {
    if (modal.type !== 'graduation') return

    const payload = {
      memberId: modal.memberId,
      isGraduated: !modal.currentStatus,
    }

    setModal({ type: 'none' })

    startTransition(() => {
      setOptimisticMembers({
        type: 'graduation',
        memberId: payload.memberId,
        isGraduated: payload.isGraduated,
      })
    })

    setGraduationMutation.mutate({
      memberId: payload.memberId,
      isGraduated: payload.isGraduated,
    })
  }, [modal, setGraduationMutation, setOptimisticMembers])

  const filteredMembers = useMemo(
    () =>
      optimisticMembers.filter((member: Member) => {
        if (hideGraduated && member.isGraduated) return false
        if (searchTerm) {
          const lowerSearch = searchTerm.toLowerCase()
          return (
            member.name.toLowerCase().includes(lowerSearch) ||
            member.channelId.toLowerCase().includes(lowerSearch) ||
            String(member.id).includes(lowerSearch) ||
            member.aliases.ko.some((alias) => alias.toLowerCase().includes(lowerSearch)) ||
            member.aliases.ja.some((alias) => alias.toLowerCase().includes(lowerSearch))
          )
        }
        return true
      }),
    [hideGraduated, optimisticMembers, searchTerm],
  )

  const sortedMembers = useMemo(
    () =>
      [...filteredMembers].sort((a: Member, b: Member) => {
        if (a.isGraduated !== b.isGraduated) {
          return a.isGraduated ? 1 : -1
        }
        return a.name.localeCompare(b.name)
      }),
    [filteredMembers],
  )

  if (isLoading) {
    return (
      <div className="text-center py-24 text-slate-500" aria-busy="true" aria-label="데이터를 불러오는 중입니다…">
        <div className="animate-spin inline-block w-8 h-8 border-4 border-sky-200 border-t-sky-500 rounded-full mb-4" />
        <p>데이터를 불러오는 중입니다…</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* 필터 및 검색 바 */}
      <div className="flex flex-col md:flex-row gap-4 items-center justify-between bg-white p-4 rounded-2xl shadow-sm border border-slate-100">
        <div className="flex items-center gap-4 w-full md:w-auto">
          <label className="flex items-center gap-2 cursor-pointer bg-slate-50 px-3 py-2 rounded-lg hover:bg-slate-100 transition-colors focus-within:ring-2 focus-within:ring-sky-200">
            <input
              type="checkbox"
              checked={hideGraduated}
              onChange={toggleHideGraduated}
              className="w-4 h-4 text-sky-600 rounded focus:ring-sky-500 border-gray-300"
            />
            <span className="text-sm font-medium text-slate-700 select-none">졸업 멤버 숨기기</span>
          </label>
          <div className="text-xs text-slate-400 font-medium bg-slate-50 px-3 py-2 rounded-lg tabular-nums">
            <span className="text-slate-900 font-bold">{numberFormatter.format(sortedMembers.length)}</span> / {numberFormatter.format(allMembers.length)} 명
          </div>
        </div>
      </div>

      <div className="flex flex-col md:flex-row gap-4 items-center justify-between">
        <Button 
          onClick={() => { setIsAddModalOpen(true); }} 
          className="gap-2 shrink-0 bg-sky-500 hover:bg-sky-600 text-white text-sm font-bold shadow-sm shadow-sky-200 focus-visible:ring-2 focus-visible:ring-sky-200"
          aria-label="새로운 멤버 추가"
        >
          <Plus size={16} aria-hidden="true" /> 멤버 추가
        </Button>

        <div className="relative w-full md:w-80">
          <Label htmlFor="member-search" className="sr-only">멤버 검색</Label>
          <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none text-slate-400">
            <Search size={16} aria-hidden="true" />
          </div>
          <input
            id="member-search"
            type="text"
            value={searchTerm}
            onChange={(e) => { setSearchTerm(e.target.value); }}
            placeholder="멤버 이름, ID, 별명 검색…"
            className="block w-full pl-10 pr-3 py-2 bg-slate-50 border border-slate-200 rounded-xl text-sm focus:outline-none focus:ring-2 focus:ring-sky-500/20 focus:border-sky-500 transition-all placeholder:text-slate-400"
          />
        </div>
      </div>


      {/* 멤버 카드 그리드 */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-5" role="list">
        {sortedMembers.map((member: Member) => (
          <div key={member.id} role="listitem">
            <MemberCard
              member={member}
              onAddAlias={handleAddAlias}
              onRemoveAlias={handleRemoveAlias}
              onToggleGraduation={handleToggleGraduation}
              onEditChannel={handleUpdateChannel}
              onEditName={handleEditName}
            />
          </div>
        ))}
        {sortedMembers.length === 0 && (
          <div className="col-span-full py-12 text-center text-slate-400 bg-slate-50 rounded-2xl border border-dashed border-slate-200">
            검색 결과가 없습니다.
          </div>
        )}
      </div>

      {/* 별명 삭제 확인 모달 */}
      <ConfirmModal
        isOpen={modal.type === 'removeAlias'}
        onClose={() => { setModal({ type: 'none' }) }}
        onConfirm={confirmRemoveAlias}
        title="별명 삭제"
        message={modal.type === 'removeAlias' ? `정말 삭제하시겠습니까?` : ''}
        confirmText="삭제"
        confirmColor="danger"
      >
        {modal.type === 'removeAlias' && (
          <div className="mt-2 p-3 bg-slate-50 rounded-lg text-center font-bold text-slate-700">
            {modal.alias}
          </div>
        )}
      </ConfirmModal>

      {/* 졸업 토글 확인 모달 */}
      <ConfirmModal
        isOpen={modal.type === 'graduation'}
        onClose={() => { setModal({ type: 'none' }) }}
        onConfirm={confirmToggleGraduation}
        title={modal.type === 'graduation' ? (modal.currentStatus ? '졸업 해제 (복귀)' : '졸업 처리') : ''}
        message={modal.type === 'graduation' ? `${modal.memberName}을(를) ${modal.currentStatus ? '졸업 해제' : '졸업 처리'}하시겠습니까?` : ''}
        confirmText="확인"
        confirmColor={modal.type === 'graduation' && modal.currentStatus ? 'primary' : 'danger'}
      />

      {/* 채널 ID 수정 모달 */}
      <ChannelEditModal
        isOpen={modal.type === 'channelEdit'}
        onClose={() => { setModal({ type: 'none' }) }}
        onSave={confirmUpdateChannel}
        memberId={modal.type === 'channelEdit' ? modal.memberId : 0}
        memberName={modal.type === 'channelEdit' ? modal.memberName : ''}
        currentChannelId={modal.type === 'channelEdit' ? modal.currentChannelId : ''}
      />

      <EditNameModal
        isOpen={modal.type === 'nameEdit'}
        onClose={() => { setModal({ type: 'none' }) }}
        onSave={confirmEditName}
        type="member"
        id={modal.type === 'nameEdit' ? String(modal.memberId) : ''}
        currentName={modal.type === 'nameEdit' ? modal.currentName : ''}
      />

      <AddMemberModal
        isOpen={isAddModalOpen}
        onClose={() => { setIsAddModalOpen(false); }}
        onAdd={(data) => {
          // 모달 데이터를 API 형식(Partial<Member>)에 맞게 변환함
          // 모달: { name, channelId, nameKo, nameJa }
          // API: aliases: { ko: [...], ja: [...] } 형태 필요
          const memberData: Partial<Member> = {
            name: data.name,
            channelId: data.channelId,
            nameKo: data.nameKo,
            nameJa: data.nameJa,
            aliases: {
              ko: data.nameKo ? [data.nameKo] : [],
              ja: data.nameJa ? [data.nameJa] : []
            },
            isGraduated: false
          }
          addMemberMutation.mutate(memberData)
        }}
      />
    </div >
  )
}

export default MembersTab
