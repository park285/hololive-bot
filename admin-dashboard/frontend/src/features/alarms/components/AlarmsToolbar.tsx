import Search from 'lucide-react/dist/esm/icons/search'
import { Card } from '@/components/ui/Card'
import { Input } from '@/components/ui/Input'
import { Label } from '@/components/ui/Label'

const numberFormatter = new Intl.NumberFormat('ko-KR')

interface AlarmsToolbarProps {
  search: string
  onSearchChange: (value: string) => void
  groupCount: number
  alarmCount: number
}

export const AlarmsToolbar = ({
  search,
  onSearchChange,
  groupCount,
  alarmCount,
}: AlarmsToolbarProps) => (
  <Card className="p-4 bg-white shadow-sm border-slate-200">
    <div className="flex flex-col md:flex-row items-center gap-4">
      <div className="relative w-full md:w-96">
        <Label htmlFor="alarm-search" className="sr-only">알람 검색</Label>
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" size={18} aria-hidden="true" />
        <Input
          id="alarm-search"
          placeholder="방 이름, 유저 이름, 멤버 이름…"
          value={search}
          onChange={(event) => { onSearchChange(event.target.value) }}
          className="pl-10 focus-visible:ring-2 focus-visible:ring-sky-200"
        />
      </div>
      <div className="text-sm text-slate-500 font-medium tabular-nums">
        총 <span className="text-slate-900 font-bold">{numberFormatter.format(groupCount)}</span>개 그룹 / <span className="text-slate-900 font-bold">{numberFormatter.format(alarmCount)}</span>개 알람
      </div>
    </div>
  </Card>
)
