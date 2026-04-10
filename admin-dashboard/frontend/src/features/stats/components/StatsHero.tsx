import Play from "lucide-react/dist/esm/icons/play";

export const StatsHero = () => (
	<div className="relative overflow-hidden rounded-3xl bg-white border border-slate-100 p-8 shadow-sm animate-fade-in-up">
		<div className="absolute top-0 right-0 w-96 h-96 bg-sky-50 rounded-full blur-3xl opacity-60 -mr-20 -mt-20 pointer-events-none" />
		<div className="absolute bottom-0 left-0 w-64 h-64 bg-cyan-50 rounded-full blur-3xl opacity-40 -ml-10 -mb-10 pointer-events-none" />

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
			</div>

			<div className="hidden md:flex items-center justify-center w-32 h-32 bg-linear-to-br from-sky-400 via-cyan-400 to-indigo-400 rounded-3xl shadow-xl shadow-sky-200/60 transform rotate-6 border-4 border-white hover:rotate-3 transition-transform duration-500">
				<Play className="w-16 h-16 text-white drop-shadow-md fill-white ml-2" />
			</div>
		</div>
	</div>
);
