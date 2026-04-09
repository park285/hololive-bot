/**
 * 멤버 관련 Mutation 훅
 * 반복되는 mutation 로직을 중앙화하여 재사용성 향상
 */

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { queryKeys } from '@/api/queryKeys'
import { membersApi } from '@/features/members/api'
import type { AddAliasRequest, Member, RemoveAliasRequest } from '@/features/members/types'
import toast from '@/lib/toast-api'

/**
 * 멤버 쿼리 무효화 헬퍼
 * 성공/실패 모두 최신 데이터로 동기화
 */
const useInvalidateMembers = () => {
    const queryClient = useQueryClient()
    return () => {
        void queryClient.invalidateQueries({ queryKey: queryKeys.members.all })
    }
}

/** 별명 추가 Mutation */
export const useAddAliasMutation = () => {
    const invalidate = useInvalidateMembers()
    return useMutation({
        mutationFn: ({ memberId, type, alias }: { memberId: number; type: 'ko' | 'ja'; alias: string }) =>
            membersApi.addAlias(memberId, { type, alias } satisfies AddAliasRequest),
        onSuccess: invalidate,
        onError: invalidate,
    })
}

/** 별명 삭제 Mutation */
export const useRemoveAliasMutation = () => {
    const invalidate = useInvalidateMembers()
    return useMutation({
        mutationFn: ({ memberId, type, alias }: { memberId: number; type: 'ko' | 'ja'; alias: string }) =>
            membersApi.removeAlias(memberId, { type, alias } satisfies RemoveAliasRequest),
        onSuccess: invalidate,
        onError: invalidate,
    })
}

/** 채널 ID 업데이트 Mutation */
export const useUpdateChannelMutation = () => {
    const invalidate = useInvalidateMembers()
    return useMutation({
        mutationFn: ({ memberId, channelId }: { memberId: number; channelId: string }) =>
            membersApi.updateChannel(memberId, { channelId }),
        onSuccess: invalidate,
        onError: invalidate,
    })
}

/** 이름 업데이트 Mutation */
export const useUpdateNameMutation = () => {
    const invalidate = useInvalidateMembers()
    return useMutation({
        mutationFn: ({ memberId, name }: { memberId: number; name: string }) =>
            membersApi.updateName(memberId, name),
        onSuccess: invalidate,
        onError: invalidate,
    })
}

/** 졸업 상태 변경 Mutation */
export const useSetGraduationMutation = () => {
    const invalidate = useInvalidateMembers()
    return useMutation({
        mutationFn: ({ memberId, isGraduated }: { memberId: number; isGraduated: boolean }) =>
            membersApi.setGraduation(memberId, { isGraduated }),
        onSuccess: invalidate,
        onError: invalidate,
    })
}

/** 멤버 추가 Mutation */
export const useAddMemberMutation = () => {
    const invalidate = useInvalidateMembers()
    return useMutation({
        mutationFn: membersApi.add,
        onSuccess: invalidate,
        onError: (err: Error) => {
            invalidate()
            toast.error(`멤버 추가 실패: ${err.message}`)
        }
    })
}

/**
 * 모든 멤버 mutation을 한번에 반환하는 통합 훅
 * 컴포넌트에서 필요한 mutation만 destructuring해서 사용
 */
export const useMemberMutations = () => ({
    addAlias: useAddAliasMutation(),
    removeAlias: useRemoveAliasMutation(),
    updateChannel: useUpdateChannelMutation(),
    updateName: useUpdateNameMutation(),
    setGraduation: useSetGraduationMutation(),
    addMember: useAddMemberMutation(),
})

/** 멤버 데이터에 대한 Optimistic Update 액션 타입 */
export type OptimisticUpdate =
    | { type: 'graduation'; memberId: number; isGraduated: boolean }
    | { type: 'addAlias'; memberId: number; aliasType: 'ko' | 'ja'; alias: string }
    | { type: 'removeAlias'; memberId: number; aliasType: 'ko' | 'ja'; alias: string }
    | { type: 'updateChannel'; memberId: number; channelId: string }
    | { type: 'updateName'; memberId: number; name: string }

/** Optimistic Update Reducer */
export const optimisticMemberReducer = (
    state: Member[],
    update: OptimisticUpdate
): Member[] => {
    switch (update.type) {
        case 'graduation':
            return state.map((m) =>
                m.id === update.memberId ? { ...m, isGraduated: update.isGraduated } : m
            )
        case 'addAlias':
            return state.map((m) =>
                m.id === update.memberId
                    ? {
                        ...m,
                        aliases: {
                            ...m.aliases,
                            [update.aliasType]: [...m.aliases[update.aliasType], update.alias],
                        },
                    }
                    : m
            )
        case 'removeAlias':
            return state.map((m) =>
                m.id === update.memberId
                    ? {
                        ...m,
                        aliases: {
                            ...m.aliases,
                            [update.aliasType]: m.aliases[update.aliasType].filter(
                                (a) => a !== update.alias
                            ),
                        },
                    }
                    : m
            )
        case 'updateChannel':
            return state.map((m) =>
                m.id === update.memberId ? { ...m, channelId: update.channelId } : m
            )
        case 'updateName':
            return state.map((m) =>
                m.id === update.memberId ? { ...m, name: update.name } : m
            )
    }
}
