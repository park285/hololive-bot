import ChevronDown from 'lucide-react/dist/esm/icons/chevron-down'
import ChevronUp from 'lucide-react/dist/esm/icons/chevron-up'
import Bell from 'lucide-react/dist/esm/icons/bell'
import Edit2 from 'lucide-react/dist/esm/icons/edit-2'
import MapPin from 'lucide-react/dist/esm/icons/map-pin'
import Trash2 from 'lucide-react/dist/esm/icons/trash-2'
import User from 'lucide-react/dist/esm/icons/user'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import type { Alarm } from '@/features/alarms/types'

const numberFormatter = new Intl.NumberFormat('ko-KR')
const GROUP_PREVIEW_COUNT = 5

export interface AlarmGroup {
  roomId: string
  roomName: string
  userId: string
  userName: string
  alarms: Alarm[]
}

interface AlarmGroupsProps {
  groups: AlarmGroup[]
  expandedGroups: Set<string>
  onToggleGroup: (groupKey: string) => void
  onDeleteAlarm: (alarm: Alarm) => void
  onEditName: (type: 'room' | 'user', id: string, currentName: string) => void
  visibleGroupCount: number
  onLoadMore: () => void
  isDeleting: boolean
}

export const AlarmGroups = ({
  groups,
  expandedGroups,
  onToggleGroup,
  onDeleteAlarm,
  onEditName,
  visibleGroupCount,
  onLoadMore,
  isDeleting,
}: AlarmGroupsProps) => {
  const visibleGroups = groups.slice(0, visibleGroupCount)

  return (
    <>
      <div className="space-y-4" role="list">
        {groups.length === 0 ? (
          <div className="text-center py-12 bg-slate-50 rounded-xl border border-dashed border-slate-200">
            <Bell className="mx-auto h-12 w-12 text-slate-300 mb-3" aria-hidden="true" />
            <h3 className="text-lg font-medium text-slate-900">알람이 없습니다</h3>
            <p className="text-slate-500">새로운 알람을 등록하거나 검색어를 변경해보세요.</p>
          </div>
        ) : (
          visibleGroups.map((group) => {
            const groupKey = `${group.roomId}:${group.userId}`
            const isExpanded = expandedGroups.has(groupKey)
            const displayAlarms = isExpanded ? group.alarms : group.alarms.slice(0, GROUP_PREVIEW_COUNT)
            const hasMore = group.alarms.length > GROUP_PREVIEW_COUNT

            return (
              <div key={groupKey} role="listitem" className="bg-white border border-slate-200 rounded-xl overflow-hidden shadow-sm transition-all hover:shadow-md focus-within:ring-2 focus-within:ring-sky-100">
                <div
                  role="button"
                  tabIndex={0}
                  aria-expanded={isExpanded}
                  onClick={() => { onToggleGroup(groupKey) }}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter' || event.key === ' ') {
                      onToggleGroup(groupKey)
                    }
                  }}
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
                          onClick={(event) => {
                            event.stopPropagation()
                            onEditName('room', group.roomId, group.roomName)
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
                          onClick={(event) => {
                            event.stopPropagation()
                            onEditName('user', group.userId, group.userName)
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

                <div className="divide-y divide-slate-100" role="list">
                  {displayAlarms.map((alarm, index) => (
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
                        onClick={(event) => {
                          event.stopPropagation()
                          onDeleteAlarm(alarm)
                        }}
                        disabled={isDeleting}
                        className="text-red-500 hover:text-red-600 hover:bg-red-50 opacity-0 group-hover:opacity-100 focus-visible:opacity-100 transition-all focus-visible:ring-2 focus-visible:ring-red-200"
                        aria-label={`${alarm.memberName || '알 수 없는 멤버'} 알람 삭제`}
                      >
                        <Trash2 size={16} aria-hidden="true" />
                      </Button>
                    </div>
                  ))}
                </div>

                {!isExpanded && hasMore && (
                  <div className="bg-slate-50/30 px-4 py-2 text-center border-t border-slate-100">
                    <button
                      onClick={(event) => {
                        event.stopPropagation()
                        onToggleGroup(groupKey)
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

      {visibleGroupCount < groups.length && (
        <div className="flex justify-center">
          <Button variant="secondary" onClick={onLoadMore} className="px-5">
            그룹 더 보기
          </Button>
        </div>
      )}
    </>
  )
}
