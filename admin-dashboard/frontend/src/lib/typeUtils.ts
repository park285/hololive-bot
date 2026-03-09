/**
 * 타입 안전성 유틸리티
 * 외부 라이브러리의 불완전한 타입 정의를 안전하게 처리하기 위한 함수들
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * 사용 목적:
 *   - 외부 라이브러리가 any 또는 unknown 타입을 반환할 때 타입 안전하게 처리
 *   - ESLint의 @typescript-eslint/no-unsafe-* 규칙을 우회하지 않고 준수
 *   - 타입 단언(as)이 필요한 로직을 이 파일로 격리하여 코드베이스 전체의 타입 안전성 유지
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * 확장 가이드 (새 함수 추가 시):
 *
 *   1. 함수명은 extract*, get*, has*, is* 패턴 사용
 *      예: extractUserId, getResponseData, hasErrorCode, isValidResponse
 *
 *   2. 입력은 반드시 unknown 타입으로 받고, 내부에서 타입 가드 수행
 *      예: function extractFoo(data: unknown): Foo | undefined
 *
 *   3. 타입 단언(as)은 타입 가드 검증 후에만 사용
 *      예: if ('foo' in data) { const { foo } = data as { foo: unknown }; }
 *
 *   4. 반환 타입은 명시적으로 선언 (타입 추론에 의존하지 않음)
 *
 *   5. JSDoc 주석으로 용도와 사용 예시 문서화
 * ─────────────────────────────────────────────────────────────────────────────
 */

/**
 * unknown 타입의 에러 데이터에서 message 속성을 안전하게 추출
 * React Router ErrorResponse.data 등 any/unknown 타입 처리용
 */
export function extractErrorMessage(data: unknown): string | undefined {
    if (typeof data === 'object' && data !== null && 'message' in data) {
        const { message } = data as { message: unknown };
        return typeof message === 'string' ? message : String(message);
    }
    return undefined;
}

/**
 * unknown 타입에서 특정 문자열 속성을 안전하게 추출
 */
export function extractStringProperty(
    data: unknown,
    key: string
): string | undefined {
    if (typeof data === 'object' && data !== null && key in data) {
        const value = (data as Record<string, unknown>)[key];
        return typeof value === 'string' ? value : undefined;
    }
    return undefined;
}

/**
 * unknown 타입이 특정 구조를 가지는지 확인하는 타입 가드
 */
export function hasProperty<K extends string>(
    data: unknown,
    key: K
): data is Record<K, unknown> {
    return typeof data === 'object' && data !== null && key in data;
}

/**
 * Error 또는 unknown에서 에러 메시지 추출
 * catch 블록에서 사용
 */
export function getErrorMessageFromUnknown(error: unknown): string {
    if (error instanceof Error) {
        return error.message;
    }
    if (typeof error === 'string') {
        return error;
    }
    const extracted = extractErrorMessage(error);
    if (extracted) {
        return extracted;
    }
    return '알 수 없는 오류가 발생했습니다.';
}
