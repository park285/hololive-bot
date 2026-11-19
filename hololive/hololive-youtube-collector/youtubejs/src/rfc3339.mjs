const rfc3339Pattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/;
const commonYearMonthDays = [31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];

/** @param {string} value */
export function hasRFC3339Shape(value) {
  return rfc3339Pattern.test(value);
}

/** @param {string} value */
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
