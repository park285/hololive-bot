import { n as e } from "./shared-t8Mukws9.mjs";
import { n as t, t as n } from "./shared-D64PDuVA.mjs";
import { t as r } from "./shared-CQbCZvXO.mjs";
import { t as i } from "./shared-BfxFa9_q.mjs";
import { n as a, t as o } from "./shared-Bi7X_D_s.mjs";
function s(e) {
	return t(e) && a(e.body);
}
var c = f, l = /* @__PURE__ */ RegExp("^[a-f0-9]{64}$", "u"), u = /* @__PURE__ */ RegExp("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$", "u"), d = e().default;
function f(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: i = e, dynamicAnchors: a = {} } = {}) {
	let o = null, s = 0, c = f.evaluated;
	c.dynamicProps && (c.props = void 0), c.dynamicItems && (c.items = void 0);
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
												if (v) {
													if (t["x-csrf-token"] !== void 0) {
														let e = t["x-csrf-token"], n = s;
														if (s === n) {
															if (typeof e == "string") {
																if (d(e) < 1) {
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
											if (t.roomId === void 0 || t.channelId === void 0) {
												let e = {};
												o === null ? o = [e] : o.push(e), s++;
											} else {
												let e = s;
												for (let e in t) if (e !== "channelId" && e !== "roomId") {
													let e = {};
													o === null ? o = [e] : o.push(e), s++;
													break;
												}
												if (e === s) {
													if (t.channelId !== void 0) {
														let e = t.channelId, n = s;
														if (s === n) {
															if (typeof e == "string") {
																if (d(e) < 1) {
																	let e = {};
																	o === null ? o = [e] : o.push(e), s++;
																}
															} else {
																let e = {};
																o === null ? o = [e] : o.push(e), s++;
															}
														}
														var y = n === s;
													} else var y = !0;
													if (y) {
														if (t.roomId !== void 0) {
															let e = t.roomId, n = s;
															if (s === n) {
																if (typeof e == "string") {
																	if (d(e) < 1) {
																		let e = {};
																		o === null ? o = [e] : o.push(e), s++;
																	}
																} else {
																	let e = {};
																	o === null ? o = [e] : o.push(e), s++;
																}
															}
															var y = n === s;
														} else var y = !0;
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
	return m === s ? (f.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (s = p, o !== null && (p ? o.length = p : o = null), f.errors = o, s === 0);
}
f.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
function p(e) {
	return t(e) && o(e.body);
}
var m = v, h = /* @__PURE__ */ RegExp("^[a-f0-9]{64}$", "u"), g = /* @__PURE__ */ RegExp("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$", "u"), _ = e().default;
function v(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: i = e, dynamicAnchors: a = {} } = {}) {
	let o = null, s = 0, c = v.evaluated;
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
															if (!h.test(e)) {
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
																if (_(e) < 1) {
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
											if (t.roomId === void 0 || t.roomName === void 0) {
												let e = {};
												o === null ? o = [e] : o.push(e), s++;
											} else {
												let e = s;
												for (let e in t) if (e !== "roomId" && e !== "roomName") {
													let e = {};
													o === null ? o = [e] : o.push(e), s++;
													break;
												}
												if (e === s) {
													if (t.roomId !== void 0) {
														let e = t.roomId, n = s;
														if (s === n) {
															if (typeof e == "string") {
																if (_(e) < 1) {
																	let e = {};
																	o === null ? o = [e] : o.push(e), s++;
																}
															} else {
																let e = {};
																o === null ? o = [e] : o.push(e), s++;
															}
														}
														var y = n === s;
													} else var y = !0;
													if (y) {
														if (t.roomName !== void 0) {
															let e = t.roomName, n = s;
															if (s === n) {
																if (typeof e == "string") {
																	if (_(e) < 1) {
																		let e = {};
																		o === null ? o = [e] : o.push(e), s++;
																	}
																} else {
																	let e = {};
																	o === null ? o = [e] : o.push(e), s++;
																}
															}
															var y = n === s;
														} else var y = !0;
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
	return u === s ? (v.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (s = l, o !== null && (l ? o.length = l : o = null), v.errors = o, s === 0);
}
v.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var y = C, b = /* @__PURE__ */ RegExp("^[a-f0-9]{64}$", "u"), x = /* @__PURE__ */ RegExp("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$", "u"), S = e().default;
function C(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: i = e, dynamicAnchors: a = {} } = {}) {
	let o = null, s = 0, c = C.evaluated;
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
															if (!b.test(e)) {
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
																if (S(e) < 1) {
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
																	if (!x.test(e)) {
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
											if (t.userId === void 0 || t.userName === void 0) {
												let e = {};
												o === null ? o = [e] : o.push(e), s++;
											} else {
												let e = s;
												for (let e in t) if (e !== "userId" && e !== "userName") {
													let e = {};
													o === null ? o = [e] : o.push(e), s++;
													break;
												}
												if (e === s) {
													if (t.userId !== void 0) {
														let e = t.userId, n = s;
														if (s === n) {
															if (typeof e == "string") {
																if (S(e) < 1) {
																	let e = {};
																	o === null ? o = [e] : o.push(e), s++;
																}
															} else {
																let e = {};
																o === null ? o = [e] : o.push(e), s++;
															}
														}
														var h = n === s;
													} else var h = !0;
													if (h) {
														if (t.userName !== void 0) {
															let e = t.userName, n = s;
															if (s === n) {
																if (typeof e == "string") {
																	if (S(e) < 1) {
																		let e = {};
																		o === null ? o = [e] : o.push(e), s++;
																	}
																} else {
																	let e = {};
																	o === null ? o = [e] : o.push(e), s++;
																}
															}
															var h = n === s;
														} else var h = !0;
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
	return u === s ? (C.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (s = l, o !== null && (l ? o.length = l : o = null), C.errors = o, s === 0);
}
C.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
export { c as request_holoDeleteAlarm, i as request_holoGetAlarms, m as request_holoSetRoomName, y as request_holoSetUserName, r as response_752e9496dd81504a, s as response_9f8aa2ce64ef8e54, n as response_b7a2836f983b1f2d, p as response_ee05f2ea05da7601 };
