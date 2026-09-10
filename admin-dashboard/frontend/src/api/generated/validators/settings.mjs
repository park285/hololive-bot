import { n as e } from "./shared-t8Mukws9.mjs";
import { n as t, t as n } from "./shared-D64PDuVA.mjs";
import { t as r } from "./shared-BfxFa9_q.mjs";
import { n as i, t as a } from "./shared-DfRRE-TP.mjs";
function o(e) {
	return t(e) && i(e.body);
}
var s = d, c = /* @__PURE__ */ RegExp("^[a-f0-9]{64}$", "u"), l = /* @__PURE__ */ RegExp("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$", "u"), u = e().default;
function d(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: i = e, dynamicAnchors: a = {} } = {}) {
	let o = null, s = 0, f = d.evaluated;
	f.dynamicProps && (f.props = void 0), f.dynamicItems && (f.items = void 0);
	let p = s, m = s, h = s, g = s;
	if (s === g) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.path === void 0 || e.query === void 0 || e.headers === void 0 || e.body === void 0) {
				let e = {};
				o === null ? o = [e] : o.push(e), s++;
			} else {
				let t = s;
				for (let t in e) if (t !== "path" && t !== "query" && t !== "headers" && t !== "body") {
					let e = {};
					o === null ? o = [e] : o.push(e), s++;
					break;
				}
				if (t === s) {
					if (e.path !== void 0) {
						let t = e.path, n = s;
						if (s === n) {
							if (t && typeof t == "object" && !Array.isArray(t)) for (let e in t) {
								let e = {};
								o === null ? o = [e] : o.push(e), s++;
								break;
							}
							else {
								let e = {};
								o === null ? o = [e] : o.push(e), s++;
							}
						}
						var _ = n === s;
					} else var _ = !0;
					if (_) {
						if (e.query !== void 0) {
							let t = e.query, n = s;
							if (s === n) {
								if (t && typeof t == "object" && !Array.isArray(t)) for (let e in t) {
									let e = {};
									o === null ? o = [e] : o.push(e), s++;
									break;
								}
								else {
									let e = {};
									o === null ? o = [e] : o.push(e), s++;
								}
							}
							var _ = n === s;
						} else var _ = !0;
						if (_) {
							if (e.headers !== void 0) {
								let t = e.headers, n = s;
								if (s === n) {
									if (t && typeof t == "object" && !Array.isArray(t)) {
										if (t["x-admin-client-generation"] === void 0 || t["x-csrf-token"] === void 0 || t["x-admin-mutation-id"] === void 0) {
											let e = {};
											o === null ? o = [e] : o.push(e), s++;
										} else {
											let e = s;
											for (let e in t) if (e !== "x-admin-client-generation" && e !== "x-csrf-token" && e !== "x-admin-mutation-id") {
												let e = {};
												o === null ? o = [e] : o.push(e), s++;
												break;
											}
											if (e === s) {
												if (t["x-admin-client-generation"] !== void 0) {
													let e = t["x-admin-client-generation"], n = s;
													if (s === n) {
														if (typeof e == "string") {
															if (!c.test(e)) {
																let e = {};
																o === null ? o = [e] : o.push(e), s++;
															}
														} else {
															let e = {};
															o === null ? o = [e] : o.push(e), s++;
														}
													}
													var v = n === s;
												} else var v = !0;
												if (v) {
													if (t["x-csrf-token"] !== void 0) {
														let e = t["x-csrf-token"], n = s;
														if (s === n) {
															if (typeof e == "string") {
																if (u(e) < 1) {
																	let e = {};
																	o === null ? o = [e] : o.push(e), s++;
																}
															} else {
																let e = {};
																o === null ? o = [e] : o.push(e), s++;
															}
														}
														var v = n === s;
													} else var v = !0;
													if (v) {
														if (t["x-admin-mutation-id"] !== void 0) {
															let e = t["x-admin-mutation-id"], n = s;
															if (s === n) {
																if (typeof e == "string") {
																	if (!l.test(e)) {
																		let e = {};
																		o === null ? o = [e] : o.push(e), s++;
																	}
																} else {
																	let e = {};
																	o === null ? o = [e] : o.push(e), s++;
																}
															}
															var v = n === s;
														} else var v = !0;
													}
												}
											}
										}
									} else {
										let e = {};
										o === null ? o = [e] : o.push(e), s++;
									}
								}
								var _ = n === s;
							} else var _ = !0;
							if (_) {
								if (e.body !== void 0) {
									let t = e.body, n = s;
									if (s === n) {
										if (t && typeof t == "object" && !Array.isArray(t)) {
											if (t.alarmAdvanceMinutes === void 0) {
												let e = {};
												o === null ? o = [e] : o.push(e), s++;
											} else {
												let e = s;
												for (let e in t) if (e !== "alarmAdvanceMinutes") {
													let e = {};
													o === null ? o = [e] : o.push(e), s++;
													break;
												}
												if (e === s && t.alarmAdvanceMinutes !== void 0) {
													let e = t.alarmAdvanceMinutes, n = s;
													if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
														let e = {};
														o === null ? o = [e] : o.push(e), s++;
													}
													if (s === n && typeof e == "number" && isFinite(e)) {
														if (e > 1440 || isNaN(e)) {
															let e = {};
															o === null ? o = [e] : o.push(e), s++;
														} else if (e < 0 || isNaN(e)) {
															let e = {};
															o === null ? o = [e] : o.push(e), s++;
														}
													}
												}
											}
										} else {
											let e = {};
											o === null ? o = [e] : o.push(e), s++;
										}
									}
									var _ = n === s;
								} else var _ = !0;
							}
						}
					}
				}
			}
		} else {
			let e = {};
			o === null ? o = [e] : o.push(e), s++;
		}
	}
	if (g === s) {
		let e = {};
		o === null ? o = [e] : o.push(e), s++;
	} else s = h, o !== null && (h ? o.length = h : o = null);
	return m === s ? (d.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (s = p, o !== null && (p ? o.length = p : o = null), d.errors = o, s === 0);
}
d.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
function f(e) {
	return t(e) && a(e.body);
}
export { r as request_holoGetSettings, s as request_holoUpdateSettings, o as response_68291d4fae3bd431, n as response_b7a2836f983b1f2d, f as response_bbe5335f8a8be186 };
