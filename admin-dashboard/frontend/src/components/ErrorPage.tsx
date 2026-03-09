import { useRouteError, isRouteErrorResponse } from 'react-router-dom';
import { RefreshCw, AlertTriangle, Home, Terminal, ShieldAlert } from 'lucide-react';
import { extractErrorMessage } from '@/lib/typeUtils';

const ErrorPage = () => {
    const error = useRouteError();
    console.error(error); // 디버깅용 로그

    let errorMessage = '예기치 않은 오류가 발생했습니다.';
    let errorTitle = '시스템 오류';

    if (isRouteErrorResponse(error)) {
        errorMessage = error.statusText || extractErrorMessage(error.data) || '페이지를 찾을 수 없습니다.';
        if (error.status === 404) {
            errorTitle = 'Page Not Found';
            errorMessage = '요청하신 페이지가 존재하지 않거나 이동되었습니다.';
        } else if (error.status === 401 || error.status === 403) {
            errorTitle = '접근 권한 없음';
            errorMessage = '이 페이지에 접근할 권한이 없습니다. 다시 로그인 해주세요.';
        }
    } else if (error instanceof Error) {
        errorMessage = error.message;
    } else if (typeof error === 'string') {
        errorMessage = error;
    }

    const handleReload = () => { window.location.reload(); };
    const handleGoHome = () => { window.location.href = '/dashboard'; };

    return (
        <div className="min-h-screen w-full flex items-center justify-center p-6 bg-[radial-gradient(ellipse_at_top_right,_var(--tw-gradient-stops))] from-indigo-100 via-slate-50 to-rose-50 font-sans overflow-hidden relative">

            {/* Background Decorative Elements */}
            <div className="absolute top-[-10%] right-[-5%] w-96 h-96 bg-purple-200/30 rounded-full blur-3xl animate-pulse" />
            <div className="absolute bottom-[-10%] left-[-5%] w-96 h-96 bg-indigo-200/30 rounded-full blur-3xl animate-pulse delay-700" />

            <div className="max-w-md w-full glass relative z-10 p-8 rounded-2xl text-center animate-in fade-in zoom-in-95 duration-500 border border-white/40 shadow-2xl shadow-indigo-500/10">

                {/* Icon Wrapper */}
                <div className="mb-8 flex justify-center">
                    <div className="relative group">
                        <div className="absolute inset-0 bg-gradient-to-tr from-rose-400 to-orange-400 rounded-full blur-xl opacity-40 group-hover:opacity-60 transition-opacity duration-500"></div>
                        <div className="relative w-20 h-20 bg-white rounded-full flex items-center justify-center shadow-lg border border-rose-50 group-hover:scale-105 transition-transform duration-300">
                            {isRouteErrorResponse(error) && error.status === 401 ? (
                                <ShieldAlert className="w-10 h-10 text-rose-500" strokeWidth={1.5} />
                            ) : (
                                <AlertTriangle className="w-10 h-10 text-rose-500" strokeWidth={1.5} />
                            )}
                        </div>
                    </div>
                </div>

                {/* Error Message */}
                <div className="space-y-3 mb-8">
                    <h1 className="text-3xl font-bold text-slate-800 tracking-tight">
                        {errorTitle}
                    </h1>
                    <p className="text-slate-500 text-base leading-relaxed break-keep">
                        {errorMessage}
                    </p>
                </div>

                {/* Action Buttons */}
                <div className="flex flex-col sm:flex-row gap-3 justify-center mb-8">
                    <button
                        onClick={handleReload}
                        className="flex-1 flex items-center justify-center gap-2 px-6 py-3 bg-white hover:bg-slate-50 text-slate-700 font-semibold rounded-xl border border-slate-200 shadow-sm transition-all duration-200 active:scale-95 group"
                    >
                        <RefreshCw className="w-4 h-4 group-hover:rotate-180 transition-transform duration-500" />
                        <span>다시 시도</span>
                    </button>

                    <button
                        onClick={handleGoHome}
                        className="flex-1 flex items-center justify-center gap-2 px-6 py-3 bg-gradient-to-r from-indigo-600 to-indigo-500 hover:from-indigo-500 hover:to-indigo-400 text-white font-semibold rounded-xl shadow-lg shadow-indigo-500/30 transition-all duration-200 active:scale-95 hover:shadow-indigo-500/40"
                    >
                        <Home className="w-4 h-4" />
                        <span>홈으로</span>
                    </button>
                </div>

                {/* Developer Debug Info */}
                {import.meta.env.DEV && error instanceof Error && (
                    <div className="mt-6 text-left animate-in slide-in-from-bottom-2 fade-in duration-500 delay-200">
                        <div className="bg-slate-950 rounded-xl overflow-hidden shadow-inner border border-slate-800/50">
                            <div className="bg-slate-900/50 px-4 py-2 border-b border-slate-800 flex items-center gap-2">
                                <Terminal className="w-3 h-3 text-slate-400" />
                                <span className="text-[10px] font-medium text-slate-400 uppercase tracking-wider">Stack Trace</span>
                            </div>
                            <div className="p-4 overflow-auto max-h-48 custom-scrollbar">
                                <code className="text-[10px] font-mono text-rose-300/90 break-all whitespace-pre-wrap leading-relaxed font-normal">
                                    {error.stack}
                                </code>
                            </div>
                        </div>
                    </div>
                )}
            </div>

            {/* Bottom Branding */}
            <div className="absolute bottom-6 text-center w-full z-10">
                <span className="text-xs font-medium text-slate-400/80 tracking-wide uppercase">
                    Hololive Kakao Bot Admin
                </span>
            </div>
        </div>
    );
};

export default ErrorPage;

