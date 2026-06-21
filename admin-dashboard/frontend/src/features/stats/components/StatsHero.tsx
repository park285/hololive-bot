import Play from "lucide-react/dist/esm/icons/play.mjs";

export const StatsHero = () => (
	<div className="relative overflow-hidden rounded-3xl bg-white border border-slate-100 p-8 shadow-sm animate-fade-in-up">
		{/* 배경 그라데이션 오브 - 깊이감 추가 */}
		<div className="absolute top-0 right-0 w-96 h-96 bg-sky-50 rounded-full blur-3xl opacity-60 -mr-20 -mt-20 pointer-events-none" />
		<div className="absolute bottom-0 left-0 w-64 h-64 bg-cyan-50 rounded-full blur-3xl opacity-40 -ml-10 -mb-10 pointer-events-none" />
		<div className="absolute top-1/2 right-1/4 w-48 h-48 bg-indigo-50 rounded-full blur-3xl opacity-30 pointer-events-none" />

		{/* 하단 그라데이션 액센트 라인 */}
		<div className="absolute bottom-0 left-0 right-0 h-1 bg-linear-to-r from-sky-400 via-cyan-400 to-indigo-400 opacity-70" />

		<div className="relative z-10 flex flex-col md:flex-row items-center justify-between gap-8">
			<div className="max-w-xl">
				<div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-sky-50 border border-sky-100 text-sky-600 text-xs font-semibold mb-4">
					<span className="relative flex h-2 w-2">
						<span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-sky-400 opacity-75" />
						<span className="relative inline-flex rounded-full h-2 w-2 bg-sky-500" />
					</span>
					System Operational
				</div>
				<h1 className="text-3xl font-display font-bold text-slate-800 tracking-tight">
					Bot Management Console
				</h1>
				<p className="text-sm text-slate-500 mt-2 leading-relaxed">
					홀로라이브 봇 운영 현황과 시스템 리소스를 한눈에 확인하세요
				</p>
			</div>

			<div className="hidden md:flex items-center justify-center w-32 h-32 bg-linear-to-br from-sky-400 via-cyan-400 to-indigo-400 rounded-3xl shadow-xl shadow-sky-200/60 transform rotate-6 border-4 border-white hover:rotate-3 transition-transform duration-500 relative">
				{/* 내부 글로우 효과 */}
				<div className="absolute inset-0 rounded-3xl bg-white/10 backdrop-blur-sm" />
				<Play className="w-16 h-16 text-white drop-shadow-md fill-white ml-2 relative z-10" />
			</div>
		</div>
	</div>
);
