import Plus from 'lucide-react/dist/esm/icons/plus'
import Search from 'lucide-react/dist/esm/icons/search'
import { Button } from '@/components/ui/Button'
import { Label } from '@/components/ui/Label'

const numberFormatter = new Intl.NumberFormat('ko-KR')

interface MembersToolbarProps {
  hideGraduated: boolean
  onToggleHideGraduated: () => void
  filteredCount: number
  totalCount: number
  onOpenAddModal: () => void
  searchTerm: string
  onSearchTermChange: (value: string) => void
}

export const MembersToolbar = ({
  hideGraduated,
  onToggleHideGraduated,
  filteredCount,
  totalCount,
  onOpenAddModal,
  searchTerm,
  onSearchTermChange,
}: MembersToolbarProps) => (
  <>
    <div className="flex flex-col md:flex-row gap-4 items-center justify-between bg-white p-4 rounded-2xl shadow-sm border border-slate-100">
      <div className="flex items-center gap-4 w-full md:w-auto">
        <label className="flex items-center gap-2 cursor-pointer bg-slate-50 px-3 py-2 rounded-lg hover:bg-slate-100 transition-colors focus-within:ring-2 focus-within:ring-sky-200">
          <input
            type="checkbox"
            checked={hideGraduated}
            onChange={onToggleHideGraduated}
            className="w-4 h-4 text-sky-600 rounded focus:ring-sky-500 border-gray-300"
          />
          <span className="text-sm font-medium text-slate-700 select-none">졸업 멤버 숨기기</span>
        </label>
        <div className="text-xs text-slate-400 font-medium bg-slate-50 px-3 py-2 rounded-lg tabular-nums">
          <span className="text-slate-900 font-bold">{numberFormatter.format(filteredCount)}</span> / {numberFormatter.format(totalCount)} 명
        </div>
      </div>
    </div>

    <div className="flex flex-col md:flex-row gap-4 items-center justify-between">
      <Button
        onClick={onOpenAddModal}
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
          onChange={(event) => { onSearchTermChange(event.target.value) }}
          placeholder="멤버 이름, ID, 별명 검색…"
          className="block w-full pl-10 pr-3 py-2 bg-slate-50 border border-slate-200 rounded-xl text-sm focus:outline-none focus:ring-2 focus:ring-sky-500/20 focus:border-sky-500 transition-all placeholder:text-slate-400"
        />
      </div>
    </div>
  </>
)
