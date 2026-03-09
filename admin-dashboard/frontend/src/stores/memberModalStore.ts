/**
 * 전역 모달 상태 관리 Store
 * 복잡한 Discriminated Union 타입 대신 개별 모달 상태로 분리
 */

import { create } from 'zustand'

/** 별명 삭제 모달 데이터 */
interface AliasRemovalData {
    memberId: number
    aliasType: 'ko' | 'ja'
    alias: string
}

/** 졸업 상태 변경 모달 데이터 */
interface GraduationData {
    memberId: number
    memberName: string
    currentStatus: boolean
}

/** 채널 ID 수정 모달 데이터 */
interface ChannelEditData {
    memberId: number
    memberName: string
    currentChannelId: string
}

/** 이름 수정 모달 데이터 */
interface NameEditData {
    memberId: number
    currentName: string
}

interface MemberModalStore {
    // 각 모달별 상태 (null이면 닫힘)
    aliasRemoval: AliasRemovalData | null
    graduation: GraduationData | null
    channelEdit: ChannelEditData | null
    nameEdit: NameEditData | null

    // 별명 삭제 모달
    openAliasRemoval: (data: AliasRemovalData) => void
    closeAliasRemoval: () => void

    // 졸업 상태 모달
    openGraduation: (data: GraduationData) => void
    closeGraduation: () => void

    // 채널 수정 모달
    openChannelEdit: (data: ChannelEditData) => void
    closeChannelEdit: () => void

    // 이름 수정 모달
    openNameEdit: (data: NameEditData) => void
    closeNameEdit: () => void

    // 모든 모달 닫기
    closeAll: () => void
}

export const useMemberModalStore = create<MemberModalStore>()((set) => ({
    // 초기 상태
    aliasRemoval: null,
    graduation: null,
    channelEdit: null,
    nameEdit: null,

    // 별명 삭제
    openAliasRemoval: (data) => { set({ aliasRemoval: data }) },
    closeAliasRemoval: () => { set({ aliasRemoval: null }) },

    // 졸업 상태
    openGraduation: (data) => { set({ graduation: data }) },
    closeGraduation: () => { set({ graduation: null }) },

    // 채널 수정
    openChannelEdit: (data) => { set({ channelEdit: data }) },
    closeChannelEdit: () => { set({ channelEdit: null }) },

    // 이름 수정
    openNameEdit: (data) => { set({ nameEdit: data }) },
    closeNameEdit: () => { set({ nameEdit: null }) },

    // 전체 닫기
    closeAll: () => {
        set({
            aliasRemoval: null,
            graduation: null,
            channelEdit: null,
            nameEdit: null,
        })
    },
}))

// 타입 export
export type { AliasRemovalData, GraduationData, ChannelEditData, NameEditData }
