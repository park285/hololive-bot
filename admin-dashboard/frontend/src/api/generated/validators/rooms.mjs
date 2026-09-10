import { n as e } from "./shared-t8Mukws9.mjs";
import { n as t, t as n } from "./shared-D64PDuVA.mjs";
import { t as r } from "./shared-CQbCZvXO.mjs";
import { t as i } from "./shared-BfxFa9_q.mjs";
import { n as a, r as o, t as s } from "./shared-LMIpDwtz.mjs";
function c(e) {
	return t(e) && o(e.body);
}
var l = p, u = /* @__PURE__ */ RegExp("^[a-f0-9]{64}$", "u"), d = /* @__PURE__ */ RegExp("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$", "u"), f = e().default;
function p(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: i = e, dynamicAnchors: a = {} } = {}) {
	let o = null, s = 0, c = p.evaluated;
	c.dynamicProps && (c.props = void 0), c.dynamicItems && (c.items = void 0);
	let l = s, m = s, h = s, g = s;
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
															if (!u.test(e)) {
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
																if (f(e) < 1) {
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
																	if (!d.test(e)) {
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
											if (t.room === void 0) {
												let e = {};
												o === null ? o = [e] : o.push(e), s++;
											} else {
												let e = s;
												for (let e in t) if (e !== "room") {
													let e = {};
													o === null ? o = [e] : o.push(e), s++;
													break;
												}
												if (e === s && t.room !== void 0) {
													let e = t.room;
													if (s === s) {
														if (typeof e == "string") {
															if (f(e) < 1) {
																let e = {};
																o === null ? o = [e] : o.push(e), s++;
															}
														} else {
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
	return m === s ? (p.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (s = l, o !== null && (l ? o.length = l : o = null), p.errors = o, s === 0);
}
p.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
function m(e) {
	return t(e) && a(e.body);
}
var h = y, g = /* @__PURE__ */ RegExp("^[a-f0-9]{64}$", "u"), _ = /* @__PURE__ */ RegExp("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$", "u"), v = e().default;
function y(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: i = e, dynamicAnchors: a = {} } = {}) {
	let o = null, s = 0, c = y.evaluated;
	c.dynamicProps && (c.props = void 0), c.dynamicItems && (c.items = void 0);
	let l = s, u = s, d = s, f = s;
	if (s === f) {
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
						var p = n === s;
					} else var p = !0;
					if (p) {
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
							var p = n === s;
						} else var p = !0;
						if (p) {
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
															if (!g.test(e)) {
																let e = {};
																o === null ? o = [e] : o.push(e), s++;
															}
														} else {
															let e = {};
															o === null ? o = [e] : o.push(e), s++;
														}
													}
													var m = n === s;
												} else var m = !0;
												if (m) {
													if (t["x-csrf-token"] !== void 0) {
														let e = t["x-csrf-token"], n = s;
														if (s === n) {
															if (typeof e == "string") {
																if (v(e) < 1) {
																	let e = {};
																	o === null ? o = [e] : o.push(e), s++;
																}
															} else {
																let e = {};
																o === null ? o = [e] : o.push(e), s++;
															}
														}
														var m = n === s;
													} else var m = !0;
													if (m) {
														if (t["x-admin-mutation-id"] !== void 0) {
															let e = t["x-admin-mutation-id"], n = s;
															if (s === n) {
																if (typeof e == "string") {
																	if (!_.test(e)) {
																		let e = {};
																		o === null ? o = [e] : o.push(e), s++;
																	}
																} else {
																	let e = {};
																	o === null ? o = [e] : o.push(e), s++;
																}
															}
															var m = n === s;
														} else var m = !0;
													}
												}
											}
										}
									} else {
										let e = {};
										o === null ? o = [e] : o.push(e), s++;
									}
								}
								var p = n === s;
							} else var p = !0;
							if (p) {
								if (e.body !== void 0) {
									let t = e.body, n = s;
									if (s === n) {
										if (t && typeof t == "object" && !Array.isArray(t)) {
											let e = s;
											for (let e in t) if (e !== "enabled" && e !== "mode") {
												let e = {};
												o === null ? o = [e] : o.push(e), s++;
												break;
											}
											if (e === s) {
												if (t.enabled !== void 0) {
													let e = t.enabled, n = s;
													if (typeof e != "boolean" && e !== null) {
														let e = {};
														o === null ? o = [e] : o.push(e), s++;
													}
													var h = n === s;
												} else var h = !0;
												if (h) {
													if (t.mode !== void 0) {
														let e = t.mode, n = s;
														if (typeof e != "string" && e !== null) {
															let e = {};
															o === null ? o = [e] : o.push(e), s++;
														}
														var h = n === s;
													} else var h = !0;
												}
											}
										} else {
											let e = {};
											o === null ? o = [e] : o.push(e), s++;
										}
									}
									var p = n === s;
								} else var p = !0;
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
	if (f === s) {
		let e = {};
		o === null ? o = [e] : o.push(e), s++;
	} else s = d, o !== null && (d ? o.length = d : o = null);
	return u === s ? (y.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (s = l, o !== null && (l ? o.length = l : o = null), y.errors = o, s === 0);
}
y.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
function b(e) {
	return t(e) && s(e.body);
}
export { l as request_holoAddRoom, l as request_holoRemoveRoom, i as request_holoGetRooms, i as request_holoGetRoomsJoined, h as request_holoSetAcl, c as response_06e829560cf77ac9, m as response_2d2c268d0d3a9dc4, r as response_752e9496dd81504a, n as response_b7a2836f983b1f2d, b as response_d2ed51f51f31f984 };
