# PATTERN-03 — HTTP handler 분리

## 권장 구조

```go
func (s *Server) Handler(w http.ResponseWriter, r *http.Request) {
    req, err := decodeHandlerRequest(r)
    if err != nil { ... }
    result, err := s.runHandlerOperation(r.Context(), req)
    if err != nil { ... }
    writeHandlerResponse(w, result)
}
```

## 불변조건

- method/path/status code 유지.
- request/response JSON field 유지.
- auth/middleware order 유지.
- error mapping 유지.
