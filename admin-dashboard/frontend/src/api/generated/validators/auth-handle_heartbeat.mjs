import { n as e } from "./shared-t8Mukws9.mjs";
import { n as t, t as n } from "./shared-D64PDuVA.mjs";
import { t as r } from "./shared-D1ZBR6bS.mjs";
var i = s, a = /* @__PURE__ */ RegExp("^[a-f0-9]{64}$", "u"), o = e().default;
function s(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: i = e, dynamicAnchors: c = {} } = {}) {
	let l = null, u = 0, d = s.evaluated;
	d.dynamicProps && (d.props = void 0), d.dynamicItems && (d.items = void 0);
	let f = u, p = u, m = u, h = u;
	if (u === h) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.path === void 0 || e.query === void 0 || e.headers === void 0) {
				let e = {};
				l === null ? l = [e] : l.push(e), u++;
			} else {
				let t = u;
				for (let t in e) if (t !== "path" && t !== "query" && t !== "headers" && t !== "body") {
					let e = {};
					l === null ? l = [e] : l.push(e), u++;
					break;
				}
				if (t === u) {
					if (e.path !== void 0) {
						let t = e.path, n = u;
						if (u === n) {
							if (t && typeof t == "object" && !Array.isArray(t)) for (let e in t) {
								let e = {};
								l === null ? l = [e] : l.push(e), u++;
								break;
							}
							else {
								let e = {};
								l === null ? l = [e] : l.push(e), u++;
							}
						}
						var g = n === u;
					} else var g = !0;
					if (g) {
						if (e.query !== void 0) {
							let t = e.query, n = u;
							if (u === n) {
								if (t && typeof t == "object" && !Array.isArray(t)) for (let e in t) {
									let e = {};
									l === null ? l = [e] : l.push(e), u++;
									break;
								}
								else {
									let e = {};
									l === null ? l = [e] : l.push(e), u++;
								}
							}
							var g = n === u;
						} else var g = !0;
						if (g) {
							if (e.headers !== void 0) {
								let t = e.headers, n = u;
								if (u === n) {
									if (t && typeof t == "object" && !Array.isArray(t)) {
										if (t["x-admin-client-generation"] === void 0 || t["x-csrf-token"] === void 0) {
											let e = {};
											l === null ? l = [e] : l.push(e), u++;
										} else {
											let e = u;
											for (let e in t) if (e !== "x-admin-client-generation" && e !== "x-csrf-token") {
												let e = {};
												l === null ? l = [e] : l.push(e), u++;
												break;
											}
											if (e === u) {
												if (t["x-admin-client-generation"] !== void 0) {
													let e = t["x-admin-client-generation"], n = u;
													if (u === n) {
														if (typeof e == "string") {
															if (!a.test(e)) {
																let e = {};
																l === null ? l = [e] : l.push(e), u++;
															}
														} else {
															let e = {};
															l === null ? l = [e] : l.push(e), u++;
														}
													}
													var _ = n === u;
												} else var _ = !0;
												if (_) {
													if (t["x-csrf-token"] !== void 0) {
														let e = t["x-csrf-token"], n = u;
														if (u === n) {
															if (typeof e == "string") {
																if (o(e) < 1) {
																	let e = {};
																	l === null ? l = [e] : l.push(e), u++;
																}
															} else {
																let e = {};
																l === null ? l = [e] : l.push(e), u++;
															}
														}
														var _ = n === u;
													} else var _ = !0;
												}
											}
										}
									} else {
										let e = {};
										l === null ? l = [e] : l.push(e), u++;
									}
								}
								var g = n === u;
							} else var g = !0;
							if (g) {
								if (e.body !== void 0) {
									let t = e.body, n = u;
									if (u === n) {
										if (t && typeof t == "object" && !Array.isArray(t)) {
											let e = u;
											for (let e in t) if (e !== "idle") {
												let e = {};
												l === null ? l = [e] : l.push(e), u++;
												break;
											}
											if (e === u && t.idle !== void 0 && typeof t.idle != "boolean") {
												let e = {};
												l === null ? l = [e] : l.push(e), u++;
											}
										} else {
											let e = {};
											l === null ? l = [e] : l.push(e), u++;
										}
									}
									var g = n === u;
								} else var g = !0;
							}
						}
					}
				}
			}
		} else {
			let e = {};
			l === null ? l = [e] : l.push(e), u++;
		}
	}
	if (h === u) {
		let e = {};
		l === null ? l = [e] : l.push(e), u++;
	} else u = m, l !== null && (m ? l.length = m : l = null);
	return p === u ? (s.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (u = f, l !== null && (f ? l.length = f : l = null), s.errors = l, u === 0);
}
s.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
function c(e) {
	return t(e) && r(e.body);
}
export { i as request_handle_heartbeat, n as response_b7a2836f983b1f2d, c as response_daff50d5a779875b };
