import { n as e } from "./shared-t8Mukws9.mjs";
var t = r, n = e().default;
function r(e, { instancePath: t = "", parentData: i, parentDataProperty: a, rootData: o = e, dynamicAnchors: s = {} } = {}) {
	let c = null, l = 0, u = r.evaluated;
	u.dynamicProps && (u.props = void 0), u.dynamicItems && (u.items = void 0);
	let d = l, f = l, p = l, m = l, h = l, g = !1, _ = null, v = l;
	if (l === v) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.status === void 0 || e.absolute_expires_at === void 0) {
				let e = {};
				c === null ? c = [e] : c.push(e), l++;
			} else {
				let t = l;
				for (let t in e) if (t !== "status" && t !== "absolute_expires_at") {
					let e = {};
					c === null ? c = [e] : c.push(e), l++;
					break;
				}
				if (t === l) {
					if (e.status !== void 0) {
						let t = e.status, n = l;
						if (typeof t != "string") {
							let e = {};
							c === null ? c = [e] : c.push(e), l++;
						}
						if (t !== "ok") {
							let e = {};
							c === null ? c = [e] : c.push(e), l++;
						}
						var y = n === l;
					} else var y = !0;
					if (y) {
						if (e.absolute_expires_at !== void 0) {
							let t = e.absolute_expires_at, n = l;
							if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
								let e = {};
								c === null ? c = [e] : c.push(e), l++;
							}
							if (l === n && typeof t == "number" && isFinite(t) && (t < 0 || isNaN(t))) {
								let e = {};
								c === null ? c = [e] : c.push(e), l++;
							}
							var y = n === l;
						} else var y = !0;
					}
				}
			}
		} else {
			let e = {};
			c === null ? c = [e] : c.push(e), l++;
		}
	}
	var b = v === l;
	if (b) {
		g = !0, _ = 0;
		var x = !0;
	}
	let S = l;
	if (l === S) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.status === void 0 || e.absolute_expires_at === void 0 || e.rotated === void 0 || e.csrf_token === void 0) {
				let e = {};
				c === null ? c = [e] : c.push(e), l++;
			} else {
				let t = l;
				for (let t in e) if (t !== "status" && t !== "absolute_expires_at" && t !== "rotated" && t !== "csrf_token") {
					let e = {};
					c === null ? c = [e] : c.push(e), l++;
					break;
				}
				if (t === l) {
					if (e.status !== void 0) {
						let t = e.status, n = l;
						if (typeof t != "string") {
							let e = {};
							c === null ? c = [e] : c.push(e), l++;
						}
						if (t !== "ok") {
							let e = {};
							c === null ? c = [e] : c.push(e), l++;
						}
						var C = n === l;
					} else var C = !0;
					if (C) {
						if (e.absolute_expires_at !== void 0) {
							let t = e.absolute_expires_at, n = l;
							if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
								let e = {};
								c === null ? c = [e] : c.push(e), l++;
							}
							if (l === n && typeof t == "number" && isFinite(t) && (t < 0 || isNaN(t))) {
								let e = {};
								c === null ? c = [e] : c.push(e), l++;
							}
							var C = n === l;
						} else var C = !0;
						if (C) {
							if (e.rotated !== void 0) {
								let t = l;
								if (!0 !== e.rotated) {
									let e = {};
									c === null ? c = [e] : c.push(e), l++;
								}
								var C = t === l;
							} else var C = !0;
							if (C) {
								if (e.csrf_token !== void 0) {
									let t = e.csrf_token, r = l;
									if (l === r) {
										if (typeof t == "string") {
											if (n(t) < 1) {
												let e = {};
												c === null ? c = [e] : c.push(e), l++;
											}
										} else {
											let e = {};
											c === null ? c = [e] : c.push(e), l++;
										}
									}
									var C = r === l;
								} else var C = !0;
							}
						}
					}
				}
			}
		} else {
			let e = {};
			c === null ? c = [e] : c.push(e), l++;
		}
	}
	var b = S === l;
	if (b && g) g = !1, _ = [_, 1];
	else {
		b && (g = !0, _ = 1, x !== !0 && (x = !0));
		let t = l;
		if (l === t) {
			if (e && typeof e == "object" && !Array.isArray(e)) {
				if (e.status === void 0 || e.idle_rejected === void 0) {
					let e = {};
					c === null ? c = [e] : c.push(e), l++;
				} else {
					let t = l;
					for (let t in e) if (t !== "status" && t !== "idle_rejected") {
						let e = {};
						c === null ? c = [e] : c.push(e), l++;
						break;
					}
					if (t === l) {
						if (e.status !== void 0) {
							let t = l;
							if (e.status !== "idle") {
								let e = {};
								c === null ? c = [e] : c.push(e), l++;
							}
							var w = t === l;
						} else var w = !0;
						if (w) {
							if (e.idle_rejected !== void 0) {
								let t = l;
								if (!0 !== e.idle_rejected) {
									let e = {};
									c === null ? c = [e] : c.push(e), l++;
								}
								var w = t === l;
							} else var w = !0;
						}
					}
				}
			} else {
				let e = {};
				c === null ? c = [e] : c.push(e), l++;
			}
		}
		var b = t === l;
		b && g ? (g = !1, _ = [_, 2]) : b && (g = !0, _ = 2, x !== !0 && (x = !0));
	}
	if (g) l = h, c !== null && (h ? c.length = h : c = null);
	else {
		let e = {};
		c === null ? c = [e] : c.push(e), l++;
	}
	if (m === l) {
		let e = {};
		c === null ? c = [e] : c.push(e), l++;
	} else l = p, c !== null && (p ? c.length = p : c = null);
	return f === l ? (r.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (l = d, c !== null && (d ? c.length = d : c = null), r.errors = c, l === 0);
}
r.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
export { t };
