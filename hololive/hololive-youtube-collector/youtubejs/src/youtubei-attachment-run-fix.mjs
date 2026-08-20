const upstreamFromAttributedByClass = new WeakMap();

// youtubei.js 18.0.0의 Text.fromAttributed는 length가 없는 attachment run을 매칭하지 못해
// 채널 페이지를 파싱할 때마다 경고와 객체 덤프를 남긴다. LuanRT/YouTube.js#1241과 같은
// 정규화를 선적용하는 shim이며, 그 수정이 포함된 버전으로 올리면 이 모듈을 제거한다.
/** @param {typeof import("youtubei.js").Misc.Text} Text */
export function applyAttachmentRunLengthFix(Text) {
  if (upstreamFromAttributedByClass.has(Text)) {
    return;
  }
  const upstream = Text.fromAttributed;
  upstreamFromAttributedByClass.set(Text, upstream);
  Text.fromAttributed = function fromAttributed(data) {
    return upstream.call(this, normalizeAttachmentRuns(data));
  };
}

/** @param {typeof import("youtubei.js").Misc.Text} Text */
export function upstreamFromAttributedOf(Text) {
  return upstreamFromAttributedByClass.get(Text);
}

function normalizeAttachmentRuns(data) {
  if (!Array.isArray(data?.attachmentRuns)) {
    return data;
  }
  return {
    ...data,
    attachmentRuns: data.attachmentRuns.map((run) => ({
      ...run,
      startIndex: run.startIndex ?? 0,
      length: run.length ?? 0,
    })),
  };
}
