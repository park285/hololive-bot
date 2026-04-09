import { Badge } from '@/components/ui/Badge'
import type { MilestonesResponse } from '@/features/milestones/types'
import { Trophy } from 'lucide-react'

interface AchievedMilestonesSectionProps {
  achievedData?: MilestonesResponse
}

export const AchievedMilestonesSection = ({ achievedData }: AchievedMilestonesSectionProps) => (
  <div className="space-y-4">
    <div className="flex items-center justify-between pb-2 border-b border-slate-200">
      <h3 className="text-lg font-bold text-slate-800 flex items-center gap-2">
        <Trophy size={20} className="text-indigo-500" />
        최근 달성 기록
      </h3>
    </div>

    <div className="bg-white rounded-2xl border border-slate-200 shadow-sm overflow-hidden">
      {(achievedData?.milestones.length ?? 0) === 0 ? (
        <div className="text-center py-12 text-slate-500">
          최근 달성 기록이 없습니다.
        </div>
      ) : (
        <div className="divide-y divide-slate-100">
          {achievedData?.milestones.map((milestone, idx) => (
            <div key={`${milestone.channelId}-${String(milestone.value)}-${String(idx)}`} className="p-4 hover:bg-slate-50 transition-colors flex items-center justify-between">
              <div className="flex items-center gap-4">
                <div className="w-10 h-10 rounded-full bg-indigo-50 text-indigo-600 flex items-center justify-center font-bold">
                  #{idx + 1}
                </div>
                <div>
                  <div className="font-bold text-slate-800">{milestone.memberName}</div>
                  <div className="text-sm text-slate-500">
                    {milestone.value.toLocaleString()} {milestone.type}
                  </div>
                </div>
              </div>
              <div className="text-right">
                <div className="text-xs text-slate-400 mb-1">
                  {new Date(milestone.achievedAt).toLocaleDateString()}
                </div>
                <Badge variant={milestone.notified ? 'default' : 'outline'} className={milestone.notified ? 'bg-emerald-500 hover:bg-emerald-600' : 'text-amber-500 border-amber-500'}>
                  {milestone.notified ? '알림 완료' : '대기 중'}
                </Badge>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  </div>
)
