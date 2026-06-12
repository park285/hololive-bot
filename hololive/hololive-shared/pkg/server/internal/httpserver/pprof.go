package httpserver

import (
	"net/http"
	"net/http/pprof"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func NewPprofServer(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	return &http.Server{
		Addr:    addr,
		Handler: mux,
		// /debug/pprof/profile?seconds=N은 N초간 블로킹 응답을 스트리밍하므로
		// Read/WriteTimeout을 두면 프로파일이 잘린다. ReadHeaderTimeout만 건다.
		ReadHeaderTimeout: constants.ServerTimeout.ReadHeader,
		MaxHeaderBytes:    constants.ServerTimeout.MaxHeaderBytes,
	}
}
