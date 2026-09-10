import { n as e, t } from "./shared-D64PDuVA.mjs";
import { t as n } from "./shared-Bp90oBTN.mjs";
var r = a, i = /* @__PURE__ */ RegExp("^[a-f0-9]{64}$", "u");
function a(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: o = e, dynamicAnchors: s = {} } = {}) {
	let c = null, l = 0, u = a.evaluated;
	u.dynamicProps && (u.props = void 0), u.dynamicItems && (u.items = void 0);
	let d = l, f = l, p = l, m = l;
	if (l === m) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.path === void 0 || e.query === void 0 || e.headers === void 0) {
				let e = {};
				c === null ? c = [e] : c.push(e), l++;
			} else {
				let t = l;
				for (let t in e) if (t !== "path" && t !== "query" && t !== "headers" && t !== "body") {
					let e = {};
					c === null ? c = [e] : c.push(e), l++;
					break;
				}
				if (t === l) {
					if (e.path !== void 0) {
						let t = e.path, n = l;
						if (l === n) {
							if (t && typeof t == "object" && !Array.isArray(t)) for (let e in t) {
								let e = {};
								c === null ? c = [e] : c.push(e), l++;
								break;
							}
							else {
								let e = {};
								c === null ? c = [e] : c.push(e), l++;
							}
						}
						var h = n === l;
					} else var h = !0;
					if (h) {
						if (e.query !== void 0) {
							let t = e.query, n = l;
							if (l === n) {
								if (t && typeof t == "object" && !Array.isArray(t)) {
									let e = l;
									for (let e in t) if (e !== "month" && e !== "year") {
										let e = {};
										c === null ? c = [e] : c.push(e), l++;
										break;
									}
									if (e === l) {
										if (t.month !== void 0) {
											let e = t.month, n = l;
											if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e)) && e !== null) {
												let e = {};
												c === null ? c = [e] : c.push(e), l++;
											}
											if (l === n && typeof e == "number" && isFinite(e)) {
												if (e > 12 || isNaN(e)) {
													let e = {};
													c === null ? c = [e] : c.push(e), l++;
												} else if (e < 1 || isNaN(e)) {
													let e = {};
													c === null ? c = [e] : c.push(e), l++;
												}
											}
											var g = n === l;
										} else var g = !0;
										if (g) {
											if (t.year !== void 0) {
												let e = t.year, n = l;
												if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e)) && e !== null) {
													let e = {};
													c === null ? c = [e] : c.push(e), l++;
												}
												if (l === n && typeof e == "number" && isFinite(e)) {
													if (e > 2100 || isNaN(e)) {
														let e = {};
														c === null ? c = [e] : c.push(e), l++;
													} else if (e < 2e3 || isNaN(e)) {
														let e = {};
														c === null ? c = [e] : c.push(e), l++;
													}
												}
												var g = n === l;
											} else var g = !0;
										}
									}
								} else {
									let e = {};
									c === null ? c = [e] : c.push(e), l++;
								}
							}
							var h = n === l;
						} else var h = !0;
						if (h) {
							if (e.headers !== void 0) {
								let t = e.headers, n = l;
								if (l === n) {
									if (t && typeof t == "object" && !Array.isArray(t)) {
										if (t["x-admin-client-generation"] === void 0) {
											let e = {};
											c === null ? c = [e] : c.push(e), l++;
										} else {
											let e = l;
											for (let e in t) if (e !== "x-admin-client-generation") {
												let e = {};
												c === null ? c = [e] : c.push(e), l++;
												break;
											}
											if (e === l && t["x-admin-client-generation"] !== void 0) {
												let e = t["x-admin-client-generation"];
												if (l === l) {
													if (typeof e == "string") {
														if (!i.test(e)) {
															let e = {};
															c === null ? c = [e] : c.push(e), l++;
														}
													} else {
														let e = {};
														c === null ? c = [e] : c.push(e), l++;
													}
												}
											}
										}
									} else {
										let e = {};
										c === null ? c = [e] : c.push(e), l++;
									}
								}
								var h = n === l;
							} else var h = !0;
							if (h) {
								if (e.body !== void 0) {
									var h = !1;
									let e = {};
									c === null ? c = [e] : c.push(e), l++;
								} else var h = !0;
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
	if (m === l) {
		let e = {};
		c === null ? c = [e] : c.push(e), l++;
	} else l = p, c !== null && (p ? c.length = p : c = null);
	return f === l ? (a.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (l = d, c !== null && (d ? c.length = d : c = null), a.errors = c, l === 0);
}
a.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
function o(t) {
	return e(t) && n(t.body);
}
export { r as request_holoGetCalendar, t as response_b7a2836f983b1f2d, o as response_cbc9e95d048967ae };
