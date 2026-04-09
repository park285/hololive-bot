import ArrowRight from 'lucide-react/dist/esm/icons/arrow-right'

interface StatsQuickLinksProps {
    onNavigate: (path: string) => void
}

const links = [
    {
        label: '멤버 관리하기',
        path: '/dashboard/members',
        className: 'bg-sky-50 text-sky-700 hover:bg-sky-100',
    },
    {
        label: '알람 설정 확인',
        path: '/dashboard/alarms',
        className: 'bg-rose-50 text-rose-700 hover:bg-rose-100',
    },
    {
        label: '채팅방 목록',
        path: '/dashboard/rooms',
        className: 'bg-indigo-50 text-indigo-700 hover:bg-indigo-100',
    },
]

export const StatsQuickLinks = ({ onNavigate }: StatsQuickLinksProps) => (
    <div className="bg-white rounded-2xl border border-slate-200 p-6 shadow-sm flex flex-col h-fit animate-fade-in-up stagger-4">
        <h3 className="text-lg font-display font-bold text-slate-800 mb-4">바로가기</h3>
        <div className="space-y-3 flex-1">
            {links.map((link) => (
                <button
                    key={link.path}
                    onClick={() => { onNavigate(link.path) }}
                    className={`w-full flex items-center justify-between p-3 rounded-xl transition-colors group text-left ${link.className}`}
                >
                    <span className="font-medium">{link.label}</span>
                    <ArrowRight size={18} className="opacity-50 group-hover:opacity-100 group-hover:translate-x-1 transition-transform" aria-hidden="true" />
                </button>
            ))}
        </div>
    </div>
)
