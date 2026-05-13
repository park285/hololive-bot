# PATTERN-09 — Literal/schema builder 분리

## 적용 대상

거대한 map literal, JSON schema builder, template sample data.

## 권장 구조

```go
func templateSampleCoreData() map[TemplateKey]any {
    data := map[TemplateKey]any{}
    addOutboxSamples(data)
    addCommandSamples(data)
    addAlarmSamples(data)
    return data
}
```

## 불변조건

- 모든 key 유지.
- nested field 이름 유지.
- value type 유지.
- returned map shape 유지.
