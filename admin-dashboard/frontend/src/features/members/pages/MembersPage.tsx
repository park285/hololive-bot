import { startTransition, useCallback, useDeferredValue, useEffect, useMemo, useOptimistic, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import AddMemberModal from '@/components/AddMemberModal'
import ChannelEditModal from '@/components/ChannelEditModal'
import { ConfirmModal } from '@/components/ConfirmModal'
import EditNameModal from '@/components/EditNameModal'
import { queryKeys } from '@/api/queryKeys'
import { useSSRData } from '@/hooks/useSSRData'
import { optimisticMemberReducer, useMemberMutations } from '@/hooks/useMemberMutations'
import { MembersGrid } from '@/features/members/components/MembersGrid'
import { MembersToolbar } from '@/features/members/components/MembersToolbar'
import { membersApi } from '@/features/members/api'
import type { Member, MembersResponse } from '@/features/members/types'

const MEMBER_PAGE_SIZE = 48

type ModalState =
  | { type: 'none' }
  | { type: 'removeAlias'; memberId: number; aliasType: 'ko' | 'ja'; alias: string }
  | { type: 'graduation'; memberId: number; memberName: string; currentStatus: boolean }
  | { type: 'channelEdit'; memberId: number; memberName: string; currentChannelId: string }
  | { type: 'nameEdit'; memberId: number; currentName: string }

export const MembersPage = () => {
  const ssrInitialData = useSSRData('members', (data) =>
    data?.status === 'ok' && data.members ? (data as MembersResponse) : undefined,
  )

  const { data: response, isLoading } = useQuery({
    queryKey: queryKeys.members.all,
    queryFn: membersApi.getAll,
    initialData: ssrInitialData,
  })

  const {
    addAlias: addAliasMutation,
    removeAlias: removeAliasMutation,
    updateChannel: updateChannelMutation,
    updateName: updateNameMutation,
    setGraduation: setGraduationMutation,
    addMember: addMemberMutation,
  } = useMemberMutations()

  const [searchTerm, setSearchTerm] = useState('')
  const deferredSearchTerm = useDeferredValue(searchTerm)
  const [hideGraduated, setHideGraduated] = useState<boolean>(() => {
    const saved = localStorage.getItem('hideGraduated')
    return saved !== null ? saved === 'true' : true
  })
  const [modal, setModal] = useState<ModalState>({ type: 'none' })
  const [isAddModalOpen, setIsAddModalOpen] = useState(false)
  const [visibleCount, setVisibleCount] = useState(MEMBER_PAGE_SIZE)

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

  const [optimisticMembers, setOptimisticMembers] = useOptimistic(allMembers, optimisticMemberReducer)

  const toggleHideGraduated = () => {
    const nextValue = !hideGraduated
    setHideGraduated(nextValue)
    localStorage.setItem('hideGraduated', String(nextValue))
  }

  useEffect(() => {
    setVisibleCount(MEMBER_PAGE_SIZE)
  }, [deferredSearchTerm, hideGraduated])

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
      optimisticMembers.filter((member) => {
        if (hideGraduated && member.isGraduated) return false
        if (deferredSearchTerm) {
          const lowerSearch = deferredSearchTerm.toLowerCase()
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
    [deferredSearchTerm, hideGraduated, optimisticMembers],
  )

  const sortedMembers = useMemo(
    () =>
      [...filteredMembers].sort((first, second) => {
        if (first.isGraduated !== second.isGraduated) {
          return first.isGraduated ? 1 : -1
        }
        return first.name.localeCompare(second.name)
      }),
    [filteredMembers],
  )

  const visibleMembers = useMemo(
    () => sortedMembers.slice(0, visibleCount),
    [sortedMembers, visibleCount],
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
      <MembersToolbar
        hideGraduated={hideGraduated}
        onToggleHideGraduated={toggleHideGraduated}
        filteredCount={sortedMembers.length}
        totalCount={allMembers.length}
        onOpenAddModal={() => { setIsAddModalOpen(true) }}
        searchTerm={searchTerm}
        onSearchTermChange={setSearchTerm}
      />

      <MembersGrid
        visibleMembers={visibleMembers}
        totalCount={sortedMembers.length}
        canLoadMore={visibleCount < sortedMembers.length}
        onLoadMore={() => { setVisibleCount((prev) => prev + MEMBER_PAGE_SIZE) }}
        onAddAlias={handleAddAlias}
        onRemoveAlias={handleRemoveAlias}
        onToggleGraduation={handleToggleGraduation}
        onEditChannel={handleUpdateChannel}
        onEditName={handleEditName}
      />

      <ConfirmModal
        isOpen={modal.type === 'removeAlias'}
        onClose={() => { setModal({ type: 'none' }) }}
        onConfirm={confirmRemoveAlias}
        title="별명 삭제"
        message={modal.type === 'removeAlias' ? '정말 삭제하시겠습니까?' : ''}
        confirmText="삭제"
        confirmColor="danger"
      >
        {modal.type === 'removeAlias' && (
          <div className="mt-2 p-3 bg-slate-50 rounded-lg text-center font-bold text-slate-700">
            {modal.alias}
          </div>
        )}
      </ConfirmModal>

      <ConfirmModal
        isOpen={modal.type === 'graduation'}
        onClose={() => { setModal({ type: 'none' }) }}
        onConfirm={confirmToggleGraduation}
        title={modal.type === 'graduation' ? (modal.currentStatus ? '졸업 해제 (복귀)' : '졸업 처리') : ''}
        message={modal.type === 'graduation' ? `${modal.memberName}을(를) ${modal.currentStatus ? '졸업 해제' : '졸업 처리'}하시겠습니까?` : ''}
        confirmText="확인"
        confirmColor={modal.type === 'graduation' && modal.currentStatus ? 'primary' : 'danger'}
      />

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
        onClose={() => { setIsAddModalOpen(false) }}
        onAdd={(data) => {
          const memberData: Partial<Member> = {
            name: data.name,
            channelId: data.channelId,
            nameKo: data.nameKo,
            nameJa: data.nameJa,
            aliases: {
              ko: data.nameKo ? [data.nameKo] : [],
              ja: data.nameJa ? [data.nameJa] : [],
            },
            isGraduated: false,
          }
          addMemberMutation.mutate(memberData)
        }}
      />
    </div>
  )
}
