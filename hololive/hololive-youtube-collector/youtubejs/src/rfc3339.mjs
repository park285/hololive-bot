// 엄격한 RFC3339 문자열 형식과 달력 범위만 판정합니다.
// 실패를 parser drift, UNKNOWN, 생략 중 무엇으로 볼지와 정규화 정책은 호출자가 소유합니다.

const rfc3339Pattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/;
const commonYearMonthDays = [31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];

/** 형식만 확인합니다. 월·일·시각 범위는 검사하지 않습니다. */
export function hasRFC3339Shape(value) {
  return rfc3339Pattern.test(value);
}

/** 형식과 함께 월·일(윤년 포함)·시·분·초 범위를 확인합니다. Date.parse 가능 여부는 호출자가 따로 판정합니다. */
export function isCalendarRFC3339(value) {
  const match = rfc3339Pattern.exec(value);
  if (match == null) {
    return false;
  }
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  if (month < 1 || month > 12 || day < 1 || Number(match[4]) > 23 || Number(match[5]) > 59 || Number(match[6]) > 59) {
    return false;
  }
  const leapFebruary = month === 2 && year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  return day <= commonYearMonthDays[month - 1] + (leapFebruary ? 1 : 0);
}
