# PATTERN-02 — Config load/validation 분리

## 적용 대상

`LoadConfig`, `Validate`, `validate*ModePair`.

## 권장 helper

```go
func loadServerConfig() ServerConfig
func loadIrisConfig() IrisConfig
func loadDispatchConfig() DispatchConfig
func normalizeDispatchMode(*DispatchConfig) error
func validateRetryConfig(DispatchConfig) error
func validateModeResources(Config) error
```

## 불변조건

- env var 이름 유지.
- fallback order 유지.
- default value 유지.
- error message prefix 유지.
- mode normalization 결과 유지.
