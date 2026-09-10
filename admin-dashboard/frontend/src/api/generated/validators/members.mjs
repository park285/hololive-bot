import { n as e } from "./shared-t8Mukws9.mjs";
import { n as t, t as n } from "./shared-D64PDuVA.mjs";
import { t as r } from "./shared-CQbCZvXO.mjs";
import { t as i } from "./shared-BfxFa9_q.mjs";
import { t as a } from "./shared-DsrfblFq.mjs";
function o(e) {
	return t(e) && a(e.body);
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
											if (t.name === void 0 || t.channelId === void 0 || t.aliases === void 0 || t.isGraduated === void 0) {
												let e = {};
												o === null ? o = [e] : o.push(e), s++;
											} else {
												let e = s;
												for (let e in t) if (e !== "aliases" && e !== "channelId" && e !== "isGraduated" && e !== "name" && e !== "nameJa" && e !== "nameKo") {
													let e = {};
													o === null ? o = [e] : o.push(e), s++;
													break;
												}
												if (e === s) {
													if (t.aliases !== void 0) {
														let e = t.aliases, n = s;
														if (s === n) {
															if (e && typeof e == "object" && !Array.isArray(e)) {
																if (e.ko === void 0 || e.ja === void 0) {
																	let e = {};
																	o === null ? o = [e] : o.push(e), s++;
																} else {
																	let t = s;
																	for (let t in e) if (t !== "ja" && t !== "ko") {
																		let e = {};
																		o === null ? o = [e] : o.push(e), s++;
																		break;
																	}
																	if (t === s) {
																		if (e.ja !== void 0) {
																			let t = e.ja, n = s;
																			if (s === n) {
																				if (Array.isArray(t)) {
																					let e = t.length;
																					for (let n = 0; n < e; n++) {
																						let e = s;
																						if (typeof t[n] != "string") {
																							let e = {};
																							o === null ? o = [e] : o.push(e), s++;
																						}
																						if (e !== s) break;
																					}
																				} else {
																					let e = {};
																					o === null ? o = [e] : o.push(e), s++;
																				}
																			}
																			var y = n === s;
																		} else var y = !0;
																		if (y) {
																			if (e.ko !== void 0) {
																				let t = e.ko, n = s;
																				if (s === n) {
																					if (Array.isArray(t)) {
																						let e = t.length;
																						for (let n = 0; n < e; n++) {
																							let e = s;
																							if (typeof t[n] != "string") {
																								let e = {};
																								o === null ? o = [e] : o.push(e), s++;
																							}
																							if (e !== s) break;
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
														var b = n === s;
													} else var b = !0;
													if (b) {
														if (t.channelId !== void 0) {
															let e = s;
															if (typeof t.channelId != "string") {
																let e = {};
																o === null ? o = [e] : o.push(e), s++;
															}
															var b = e === s;
														} else var b = !0;
														if (b) {
															if (t.isGraduated !== void 0) {
																let e = s;
																if (typeof t.isGraduated != "boolean") {
																	let e = {};
																	o === null ? o = [e] : o.push(e), s++;
																}
																var b = e === s;
															} else var b = !0;
															if (b) {
																if (t.name !== void 0) {
																	let e = s;
																	if (typeof t.name != "string") {
																		let e = {};
																		o === null ? o = [e] : o.push(e), s++;
																	}
																	var b = e === s;
																} else var b = !0;
																if (b) {
																	if (t.nameJa !== void 0) {
																		let e = t.nameJa, n = s;
																		if (typeof e != "string" && e !== null) {
																			let e = {};
																			o === null ? o = [e] : o.push(e), s++;
																		}
																		var b = n === s;
																	} else var b = !0;
																	if (b) {
																		if (t.nameKo !== void 0) {
																			let e = t.nameKo, n = s;
																			if (typeof e != "string" && e !== null) {
																				let e = {};
																				o === null ? o = [e] : o.push(e), s++;
																			}
																			var b = n === s;
																		} else var b = !0;
																	}
																}
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
var f = _, p = /* @__PURE__ */ RegExp("^[1-9][0-9]*$", "u"), m = /* @__PURE__ */ RegExp("^[a-f0-9]{64}$", "u"), h = /* @__PURE__ */ RegExp("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$", "u"), g = e().default;
function _(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: i = e, dynamicAnchors: a = {} } = {}) {
	let o = null, s = 0, c = _.evaluated;
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
							if (t && typeof t == "object" && !Array.isArray(t)) {
								if (t.id === void 0) {
									let e = {};
									o === null ? o = [e] : o.push(e), s++;
								} else {
									let e = s;
									for (let e in t) if (e !== "id") {
										let e = {};
										o === null ? o = [e] : o.push(e), s++;
										break;
									}
									if (e === s && t.id !== void 0) {
										let e = t.id;
										if (s === s) {
											if (typeof e == "string") {
												if (!p.test(e)) {
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
						var v = n === s;
					} else var v = !0;
					if (v) {
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
							var v = n === s;
						} else var v = !0;
						if (v) {
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
															if (!m.test(e)) {
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
													if (t["x-csrf-token"] !== void 0) {
														let e = t["x-csrf-token"], n = s;
														if (s === n) {
															if (typeof e == "string") {
																if (g(e) < 1) {
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
														if (t["x-admin-mutation-id"] !== void 0) {
															let e = t["x-admin-mutation-id"], n = s;
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
															var y = n === s;
														} else var y = !0;
													}
												}
											}
										}
									} else {
										let e = {};
										o === null ? o = [e] : o.push(e), s++;
									}
								}
								var v = n === s;
							} else var v = !0;
							if (v) {
								if (e.body !== void 0) {
									let t = e.body, n = s;
									if (s === n) {
										if (t && typeof t == "object" && !Array.isArray(t)) {
											if (t.type === void 0 || t.alias === void 0) {
												let e = {};
												o === null ? o = [e] : o.push(e), s++;
											} else {
												let e = s;
												for (let e in t) if (e !== "alias" && e !== "type") {
													let e = {};
													o === null ? o = [e] : o.push(e), s++;
													break;
												}
												if (e === s) {
													if (t.alias !== void 0) {
														let e = s;
														if (typeof t.alias != "string") {
															let e = {};
															o === null ? o = [e] : o.push(e), s++;
														}
														var b = e === s;
													} else var b = !0;
													if (b) {
														if (t.type !== void 0) {
															let e = s;
															if (typeof t.type != "string") {
																let e = {};
																o === null ? o = [e] : o.push(e), s++;
															}
															var b = e === s;
														} else var b = !0;
													}
												}
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
	return u === s ? (_.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (s = l, o !== null && (l ? o.length = l : o = null), _.errors = o, s === 0);
}
_.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var v = C, y = /* @__PURE__ */ RegExp("^[1-9][0-9]*$", "u"), b = /* @__PURE__ */ RegExp("^[a-f0-9]{64}$", "u"), x = /* @__PURE__ */ RegExp("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$", "u"), S = e().default;
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
							if (t && typeof t == "object" && !Array.isArray(t)) {
								if (t.id === void 0) {
									let e = {};
									o === null ? o = [e] : o.push(e), s++;
								} else {
									let e = s;
									for (let e in t) if (e !== "id") {
										let e = {};
										o === null ? o = [e] : o.push(e), s++;
										break;
									}
									if (e === s && t.id !== void 0) {
										let e = t.id;
										if (s === s) {
											if (typeof e == "string") {
												if (!y.test(e)) {
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
											if (t.type === void 0 || t.alias === void 0) {
												let e = {};
												o === null ? o = [e] : o.push(e), s++;
											} else {
												let e = s;
												for (let e in t) if (e !== "alias" && e !== "type") {
													let e = {};
													o === null ? o = [e] : o.push(e), s++;
													break;
												}
												if (e === s) {
													if (t.alias !== void 0) {
														let e = s;
														if (typeof t.alias != "string") {
															let e = {};
															o === null ? o = [e] : o.push(e), s++;
														}
														var h = e === s;
													} else var h = !0;
													if (h) {
														if (t.type !== void 0) {
															let e = s;
															if (typeof t.type != "string") {
																let e = {};
																o === null ? o = [e] : o.push(e), s++;
															}
															var h = e === s;
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
var w = k, T = /* @__PURE__ */ RegExp("^[1-9][0-9]*$", "u"), E = /* @__PURE__ */ RegExp("^[a-f0-9]{64}$", "u"), D = /* @__PURE__ */ RegExp("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$", "u"), O = e().default;
function k(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: i = e, dynamicAnchors: a = {} } = {}) {
	let o = null, s = 0, c = k.evaluated;
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
							if (t && typeof t == "object" && !Array.isArray(t)) {
								if (t.id === void 0) {
									let e = {};
									o === null ? o = [e] : o.push(e), s++;
								} else {
									let e = s;
									for (let e in t) if (e !== "id") {
										let e = {};
										o === null ? o = [e] : o.push(e), s++;
										break;
									}
									if (e === s && t.id !== void 0) {
										let e = t.id;
										if (s === s) {
											if (typeof e == "string") {
												if (!T.test(e)) {
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
															if (!E.test(e)) {
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
																if (O(e) < 1) {
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
																	if (!D.test(e)) {
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
											if (t.channelId === void 0) {
												let e = {};
												o === null ? o = [e] : o.push(e), s++;
											} else {
												let e = s;
												for (let e in t) if (e !== "channelId") {
													let e = {};
													o === null ? o = [e] : o.push(e), s++;
													break;
												}
												if (e === s && t.channelId !== void 0) {
													let e = t.channelId;
													if (s === s) {
														if (typeof e == "string") {
															if (O(e) < 1) {
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
	return u === s ? (k.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (s = l, o !== null && (l ? o.length = l : o = null), k.errors = o, s === 0);
}
k.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var A = F, j = /* @__PURE__ */ RegExp("^[1-9][0-9]*$", "u"), M = /* @__PURE__ */ RegExp("^[a-f0-9]{64}$", "u"), N = /* @__PURE__ */ RegExp("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$", "u"), P = e().default;
function F(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: i = e, dynamicAnchors: a = {} } = {}) {
	let o = null, s = 0, c = F.evaluated;
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
							if (t && typeof t == "object" && !Array.isArray(t)) {
								if (t.id === void 0) {
									let e = {};
									o === null ? o = [e] : o.push(e), s++;
								} else {
									let e = s;
									for (let e in t) if (e !== "id") {
										let e = {};
										o === null ? o = [e] : o.push(e), s++;
										break;
									}
									if (e === s && t.id !== void 0) {
										let e = t.id;
										if (s === s) {
											if (typeof e == "string") {
												if (!j.test(e)) {
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
															if (!M.test(e)) {
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
																if (P(e) < 1) {
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
																	if (!N.test(e)) {
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
											if (t.isGraduated === void 0) {
												let e = {};
												o === null ? o = [e] : o.push(e), s++;
											} else {
												let e = s;
												for (let e in t) if (e !== "isGraduated") {
													let e = {};
													o === null ? o = [e] : o.push(e), s++;
													break;
												}
												if (e === s && t.isGraduated !== void 0 && typeof t.isGraduated != "boolean") {
													let e = {};
													o === null ? o = [e] : o.push(e), s++;
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
	return u === s ? (F.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (s = l, o !== null && (l ? o.length = l : o = null), F.errors = o, s === 0);
}
F.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var I = V, L = /* @__PURE__ */ RegExp("^[1-9][0-9]*$", "u"), R = /* @__PURE__ */ RegExp("^[a-f0-9]{64}$", "u"), z = /* @__PURE__ */ RegExp("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$", "u"), B = e().default;
function V(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: i = e, dynamicAnchors: a = {} } = {}) {
	let o = null, s = 0, c = V.evaluated;
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
							if (t && typeof t == "object" && !Array.isArray(t)) {
								if (t.id === void 0) {
									let e = {};
									o === null ? o = [e] : o.push(e), s++;
								} else {
									let e = s;
									for (let e in t) if (e !== "id") {
										let e = {};
										o === null ? o = [e] : o.push(e), s++;
										break;
									}
									if (e === s && t.id !== void 0) {
										let e = t.id;
										if (s === s) {
											if (typeof e == "string") {
												if (!L.test(e)) {
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
															if (!R.test(e)) {
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
																if (B(e) < 1) {
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
																	if (!z.test(e)) {
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
											if (t.name === void 0) {
												let e = {};
												o === null ? o = [e] : o.push(e), s++;
											} else {
												let e = s;
												for (let e in t) if (e !== "name") {
													let e = {};
													o === null ? o = [e] : o.push(e), s++;
													break;
												}
												if (e === s && t.name !== void 0) {
													let e = t.name;
													if (s === s) {
														if (typeof e == "string") {
															if (B(e) < 1) {
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
	return u === s ? (V.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (s = l, o !== null && (l ? o.length = l : o = null), V.errors = o, s === 0);
}
V.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
export { f as request_holoAddAlias, s as request_holoAddMember, i as request_holoGetMembers, v as request_holoRemoveAlias, A as request_holoSetGraduation, w as request_holoUpdateChannel, I as request_holoUpdateMemberName, r as response_752e9496dd81504a, n as response_b7a2836f983b1f2d, o as response_db38ab7f5a37380b };
