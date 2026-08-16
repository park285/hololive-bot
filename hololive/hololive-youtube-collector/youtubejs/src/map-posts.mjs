export function textOf(value) {
  if (value == null) {
    return "";
  }
  if (typeof value === "string") {
    return value;
  }
  if (typeof value === "number" && Number.isFinite(value)) {
    return String(value);
  }
  if (typeof value.text === "string") {
    return value.text;
  }
  if (typeof value.toString === "function" && value.toString !== Object.prototype.toString) {
    const rendered = value.toString();
    if (rendered && rendered !== "[object Object]") {
      return rendered;
    }
  }
  return "";
}

export function parseShortNumber(text) {
  let value = String(text ?? "").trim();
  if (value === "" || value === "No") {
    return 0;
  }
  let multiplier = 1;
  /** @type {Array<[string, number]>} */
  const suffixes = [
    ["K", 1_000],
    ["M", 1_000_000],
    ["B", 1_000_000_000],
  ];
  for (const [suffix, factor] of suffixes) {
    if (value.endsWith(suffix)) {
      value = value.slice(0, -suffix.length);
      multiplier = factor;
      break;
    }
  }
  const parsed = Number.parseFloat(value.replaceAll(",", ""));
  if (!Number.isFinite(parsed)) {
    return 0;
  }
  return Math.trunc(parsed * multiplier);
}

export function thumbnailsOf(value) {
  const rows = Array.isArray(value)
    ? value
    : Array.isArray(value?.thumbnails)
      ? value.thumbnails
      : Array.isArray(value?.sources)
        ? value.sources
        : [];
  const mapped = [];
  for (const row of rows) {
    const rawUrl = textOf(row?.url).trim();
    if (rawUrl === "") {
      continue;
    }
    const url = rawUrl.startsWith("//") ? `https:${rawUrl}` : rawUrl;
    mapped.push({
      url,
      width: toDimension(row?.width),
      height: toDimension(row?.height),
    });
  }
  return mapped;
}

function toDimension(value) {
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed < 0) {
    return 0;
  }
  return Math.trunc(parsed);
}

export function mapPost(post) {
  if (post == null) {
    return null;
  }
  const postId = textOf(post.id || post.postId).trim();
  if (postId === "") {
    return null;
  }
  const attachment = mapAttachment(post.attachment);
  return {
    postId,
    upstreamPostId: postId,
    authorId: textOf(post.author?.id).trim(),
    authorName: textOf(post.author?.name).trim(),
    authorPhoto: thumbnailsOf(post.author?.thumbnails),
    contentText: textOf(post.content),
    publishedText: textOf(post.published),
    likeCount: parseShortNumber(textOf(post.vote_count ?? post.voteCount)),
    commentCount: commentCountOf(post),
    images: attachment.images,
    videoId: attachment.videoId,
  };
}

function commentCountOf(post) {
  const candidates = [
    post.comment_count,
    post.commentCount,
    post.action_buttons?.comment_count,
    post.action_buttons?.reply_button?.text,
    post.action_buttons?.replyButton?.text,
  ];
  for (const candidate of candidates) {
    const parsed = parseShortNumber(textOf(candidate));
    if (parsed > 0) {
      return parsed;
    }
  }
  return 0;
}

function mapAttachment(attachment) {
  if (attachment == null) {
    return { images: [], videoId: "" };
  }
  const type = textOf(attachment.type || attachment.constructor?.type);
  const videoId = textOf(
    attachment.video_id || (type === "Video" || type === "CompactVideo" ? attachment.id : ""),
  ).trim();
  const images = thumbnailsOf(
    attachment.image || attachment.images || attachment.thumbnails,
  );
  return { images, videoId };
}

export function mapPosts(posts, maxResults) {
  const limit = Number.isFinite(maxResults) && maxResults > 0 ? Math.trunc(maxResults) : 10;
  const mapped = [];
  for (const post of Array.isArray(posts) ? posts : []) {
    if (mapped.length >= limit) {
      break;
    }
    const mappedPost = mapPost(post);
    if (mappedPost != null) {
      mapped.push(mappedPost);
    }
  }
  return mapped;
}
