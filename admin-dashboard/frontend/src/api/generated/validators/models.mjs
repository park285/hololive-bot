import { n as e, t } from "./shared-t8Mukws9.mjs";
import { t as n } from "./shared-D1ZBR6bS.mjs";
import { t as r } from "./shared-D8KRab0_.mjs";
import { t as ee } from "./shared-CNeXrRES.mjs";
import { t as te } from "./shared-D4R7veKC.mjs";
import { n as i, t as a } from "./shared-vltKSu2K.mjs";
import { n as o, t as s } from "./shared-Bi7X_D_s.mjs";
import { t as c } from "./shared-DsrfblFq.mjs";
import { t as l } from "./shared-Bp90oBTN.mjs";
import { n as u, r as d, t as f } from "./shared-LMIpDwtz.mjs";
import { n as p, t as ne } from "./shared-DfRRE-TP.mjs";
import { n as re, t as ie } from "./shared-CgF-utev.mjs";
import { n as ae, t as oe } from "./shared-Bpz3s0Bh.mjs";
import { t as se } from "./shared-DOgmRKPT.mjs";
import { t as ce } from "./shared-CDcKZGHg.mjs";
import { t as m } from "./shared-BKE2KLNW.mjs";
var h = g;
function g(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = g.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.type === void 0 || e.alias === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "alias" && t !== "type") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.alias !== void 0) {
						let t = a;
						if (typeof e.alias != "string") {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
						var d = t === a;
					} else var d = !0;
					if (d) {
						if (e.type !== void 0) {
							let t = a;
							if (typeof e.type != "string") {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
							var d = t === a;
						} else var d = !0;
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (g.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), g.errors = i, a === 0);
}
g.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var _ = v;
function v(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = v.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.name === void 0 || e.channelId === void 0 || e.aliases === void 0 || e.isGraduated === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "aliases" && t !== "channelId" && t !== "isGraduated" && t !== "name" && t !== "nameJa" && t !== "nameKo") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.aliases !== void 0) {
						let t = e.aliases, n = a;
						if (a === n) {
							if (t && typeof t == "object" && !Array.isArray(t)) {
								if (t.ko === void 0 || t.ja === void 0) {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								} else {
									let e = a;
									for (let e in t) if (e !== "ja" && e !== "ko") {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
										break;
									}
									if (e === a) {
										if (t.ja !== void 0) {
											let e = t.ja, n = a;
											if (a === n) {
												if (Array.isArray(e)) {
													let t = e.length;
													for (let n = 0; n < t; n++) {
														let t = a;
														if (typeof e[n] != "string") {
															let e = {};
															i === null ? i = [e] : i.push(e), a++;
														}
														if (t !== a) break;
													}
												} else {
													let e = {};
													i === null ? i = [e] : i.push(e), a++;
												}
											}
											var d = n === a;
										} else var d = !0;
										if (d) {
											if (t.ko !== void 0) {
												let e = t.ko, n = a;
												if (a === n) {
													if (Array.isArray(e)) {
														let t = e.length;
														for (let n = 0; n < t; n++) {
															let t = a;
															if (typeof e[n] != "string") {
																let e = {};
																i === null ? i = [e] : i.push(e), a++;
															}
															if (t !== a) break;
														}
													} else {
														let e = {};
														i === null ? i = [e] : i.push(e), a++;
													}
												}
												var d = n === a;
											} else var d = !0;
										}
									}
								}
							} else {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
						}
						var f = n === a;
					} else var f = !0;
					if (f) {
						if (e.channelId !== void 0) {
							let t = a;
							if (typeof e.channelId != "string") {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
							var f = t === a;
						} else var f = !0;
						if (f) {
							if (e.isGraduated !== void 0) {
								let t = a;
								if (typeof e.isGraduated != "boolean") {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
								var f = t === a;
							} else var f = !0;
							if (f) {
								if (e.name !== void 0) {
									let t = a;
									if (typeof e.name != "string") {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
									var f = t === a;
								} else var f = !0;
								if (f) {
									if (e.nameJa !== void 0) {
										let t = e.nameJa, n = a;
										if (typeof t != "string" && t !== null) {
											let e = {};
											i === null ? i = [e] : i.push(e), a++;
										}
										var f = n === a;
									} else var f = !0;
									if (f) {
										if (e.nameKo !== void 0) {
											let t = e.nameKo, n = a;
											if (typeof t != "string" && t !== null) {
												let e = {};
												i === null ? i = [e] : i.push(e), a++;
											}
											var f = n === a;
										} else var f = !0;
									}
								}
							}
						}
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (v.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), v.errors = i, a === 0);
}
v.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var y = b, le = e().default;
function b(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = b.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.room === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "room") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a && e.room !== void 0) {
					let t = e.room;
					if (a === a) {
						if (typeof t == "string") {
							if (le(t) < 1) {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
						} else {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (b.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), b.errors = i, a === 0);
}
b.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var ue = x;
function x(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = x.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.roomId === void 0 || e.roomName === void 0 || e.channelId === void 0 || e.memberName === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "channelId" && t !== "memberName" && t !== "roomId" && t !== "roomName") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.channelId !== void 0) {
						let t = a;
						if (typeof e.channelId != "string") {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
						var d = t === a;
					} else var d = !0;
					if (d) {
						if (e.memberName !== void 0) {
							let t = a;
							if (typeof e.memberName != "string") {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
							var d = t === a;
						} else var d = !0;
						if (d) {
							if (e.roomId !== void 0) {
								let t = a;
								if (typeof e.roomId != "string") {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
								var d = t === a;
							} else var d = !0;
							if (d) {
								if (e.roomName !== void 0) {
									let t = a;
									if (typeof e.roomName != "string") {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
									var d = t === a;
								} else var d = !0;
							}
						}
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (x.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), x.errors = i, a === 0);
}
x.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var de = S;
function S(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = S.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.ko === void 0 || e.ja === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "ja" && t !== "ko") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.ja !== void 0) {
						let t = e.ja, n = a;
						if (a === n) {
							if (Array.isArray(t)) {
								let e = t.length;
								for (let n = 0; n < e; n++) {
									let e = a;
									if (typeof t[n] != "string") {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
									if (e !== a) break;
								}
							} else {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
						}
						var d = n === a;
					} else var d = !0;
					if (d) {
						if (e.ko !== void 0) {
							let t = e.ko, n = a;
							if (a === n) {
								if (Array.isArray(t)) {
									let e = t.length;
									for (let n = 0; n < e; n++) {
										let e = a;
										if (typeof t[n] != "string") {
											let e = {};
											i === null ? i = [e] : i.push(e), a++;
										}
										if (e !== a) break;
									}
								} else {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
							}
							var d = n === a;
						} else var d = !0;
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (S.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), S.errors = i, a === 0);
}
S.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var fe = C, pe = {
	$id: "urn:hololive:admin:assertion:b971d2dbf6286cfcf1d598f87fb2ff4f592ae4d61fb977e4267cd62fc5daef95",
	not: { not: {
		type: "object",
		required: [
			"kind",
			"member",
			"day"
		],
		properties: {
			day: {
				type: "integer",
				minimum: -2147483648,
				maximum: 2147483647
			},
			kind: { type: "string" },
			member: {
				type: "object",
				required: [
					"id",
					"channelId",
					"name"
				],
				properties: {
					channelId: { type: "string" },
					id: {
						type: "string",
						pattern: "^[1-9][0-9]*$"
					},
					isGraduated: { type: "boolean" },
					name: { type: "string" },
					nameKo: { type: ["string", "null"] },
					org: { type: ["string", "null"] },
					photo: { type: ["string", "null"] },
					shortKoreanName: { type: ["string", "null"] },
					suborg: { type: ["string", "null"] }
				},
				additionalProperties: !1
			},
			ordinal: {
				type: ["integer", "null"],
				minimum: -2147483648,
				maximum: 2147483647
			}
		},
		additionalProperties: !1
	} }
}, me = Object.prototype.hasOwnProperty, he = /* @__PURE__ */ RegExp("^[1-9][0-9]*$", "u");
function C(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = C.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.kind === void 0 || e.member === void 0 || e.day === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "day" && t !== "kind" && t !== "member" && t !== "ordinal") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.day !== void 0) {
						let t = e.day, n = a;
						if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
						if (a === n && typeof t == "number" && isFinite(t)) {
							if (t > 2147483647 || isNaN(t)) {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							} else if (t < -2147483648 || isNaN(t)) {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
						}
						var d = n === a;
					} else var d = !0;
					if (d) {
						if (e.kind !== void 0) {
							let t = a;
							if (typeof e.kind != "string") {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
							var d = t === a;
						} else var d = !0;
						if (d) {
							if (e.member !== void 0) {
								let t = e.member, n = a;
								if (a === n) {
									if (t && typeof t == "object" && !Array.isArray(t)) {
										if (t.id === void 0 || t.channelId === void 0 || t.name === void 0) {
											let e = {};
											i === null ? i = [e] : i.push(e), a++;
										} else {
											let e = a;
											for (let e in t) if (!me.call(pe.not.not.properties.member.properties, e)) {
												let e = {};
												i === null ? i = [e] : i.push(e), a++;
												break;
											}
											if (e === a) {
												if (t.channelId !== void 0) {
													let e = a;
													if (typeof t.channelId != "string") {
														let e = {};
														i === null ? i = [e] : i.push(e), a++;
													}
													var f = e === a;
												} else var f = !0;
												if (f) {
													if (t.id !== void 0) {
														let e = t.id, n = a;
														if (a === n) {
															if (typeof e == "string") {
																if (!he.test(e)) {
																	let e = {};
																	i === null ? i = [e] : i.push(e), a++;
																}
															} else {
																let e = {};
																i === null ? i = [e] : i.push(e), a++;
															}
														}
														var f = n === a;
													} else var f = !0;
													if (f) {
														if (t.isGraduated !== void 0) {
															let e = a;
															if (typeof t.isGraduated != "boolean") {
																let e = {};
																i === null ? i = [e] : i.push(e), a++;
															}
															var f = e === a;
														} else var f = !0;
														if (f) {
															if (t.name !== void 0) {
																let e = a;
																if (typeof t.name != "string") {
																	let e = {};
																	i === null ? i = [e] : i.push(e), a++;
																}
																var f = e === a;
															} else var f = !0;
															if (f) {
																if (t.nameKo !== void 0) {
																	let e = t.nameKo, n = a;
																	if (typeof e != "string" && e !== null) {
																		let e = {};
																		i === null ? i = [e] : i.push(e), a++;
																	}
																	var f = n === a;
																} else var f = !0;
																if (f) {
																	if (t.org !== void 0) {
																		let e = t.org, n = a;
																		if (typeof e != "string" && e !== null) {
																			let e = {};
																			i === null ? i = [e] : i.push(e), a++;
																		}
																		var f = n === a;
																	} else var f = !0;
																	if (f) {
																		if (t.photo !== void 0) {
																			let e = t.photo, n = a;
																			if (typeof e != "string" && e !== null) {
																				let e = {};
																				i === null ? i = [e] : i.push(e), a++;
																			}
																			var f = n === a;
																		} else var f = !0;
																		if (f) {
																			if (t.shortKoreanName !== void 0) {
																				let e = t.shortKoreanName, n = a;
																				if (typeof e != "string" && e !== null) {
																					let e = {};
																					i === null ? i = [e] : i.push(e), a++;
																				}
																				var f = n === a;
																			} else var f = !0;
																			if (f) {
																				if (t.suborg !== void 0) {
																					let e = t.suborg, n = a;
																					if (typeof e != "string" && e !== null) {
																						let e = {};
																						i === null ? i = [e] : i.push(e), a++;
																					}
																					var f = n === a;
																				} else var f = !0;
																			}
																		}
																	}
																}
															}
														}
													}
												}
											}
										}
									} else {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
								}
								var d = n === a;
							} else var d = !0;
							if (d) {
								if (e.ordinal !== void 0) {
									let t = e.ordinal, n = a;
									if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t)) && t !== null) {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
									if (a === n && typeof t == "number" && isFinite(t)) {
										if (t > 2147483647 || isNaN(t)) {
											let e = {};
											i === null ? i = [e] : i.push(e), a++;
										} else if (t < -2147483648 || isNaN(t)) {
											let e = {};
											i === null ? i = [e] : i.push(e), a++;
										}
									}
									var d = n === a;
								} else var d = !0;
							}
						}
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (C.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), C.errors = i, a === 0);
}
C.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var ge = w, _e = {
	$id: "urn:hololive:admin:assertion:6254a33876124002598a19dddbf413069f8b4393f6681af61bcb52504919ddaa",
	not: { not: {
		type: "object",
		required: [
			"id",
			"channelId",
			"name"
		],
		properties: {
			channelId: { type: "string" },
			id: {
				type: "string",
				pattern: "^[1-9][0-9]*$"
			},
			isGraduated: { type: "boolean" },
			name: { type: "string" },
			nameKo: { type: ["string", "null"] },
			org: { type: ["string", "null"] },
			photo: { type: ["string", "null"] },
			shortKoreanName: { type: ["string", "null"] },
			suborg: { type: ["string", "null"] }
		},
		additionalProperties: !1
	} }
}, ve = Object.prototype.hasOwnProperty, ye = /* @__PURE__ */ RegExp("^[1-9][0-9]*$", "u");
function w(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = w.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.id === void 0 || e.channelId === void 0 || e.name === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (!ve.call(_e.not.not.properties, t)) {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.channelId !== void 0) {
						let t = a;
						if (typeof e.channelId != "string") {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
						var d = t === a;
					} else var d = !0;
					if (d) {
						if (e.id !== void 0) {
							let t = e.id, n = a;
							if (a === n) {
								if (typeof t == "string") {
									if (!ye.test(t)) {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
								} else {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
							}
							var d = n === a;
						} else var d = !0;
						if (d) {
							if (e.isGraduated !== void 0) {
								let t = a;
								if (typeof e.isGraduated != "boolean") {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
								var d = t === a;
							} else var d = !0;
							if (d) {
								if (e.name !== void 0) {
									let t = a;
									if (typeof e.name != "string") {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
									var d = t === a;
								} else var d = !0;
								if (d) {
									if (e.nameKo !== void 0) {
										let t = e.nameKo, n = a;
										if (typeof t != "string" && t !== null) {
											let e = {};
											i === null ? i = [e] : i.push(e), a++;
										}
										var d = n === a;
									} else var d = !0;
									if (d) {
										if (e.org !== void 0) {
											let t = e.org, n = a;
											if (typeof t != "string" && t !== null) {
												let e = {};
												i === null ? i = [e] : i.push(e), a++;
											}
											var d = n === a;
										} else var d = !0;
										if (d) {
											if (e.photo !== void 0) {
												let t = e.photo, n = a;
												if (typeof t != "string" && t !== null) {
													let e = {};
													i === null ? i = [e] : i.push(e), a++;
												}
												var d = n === a;
											} else var d = !0;
											if (d) {
												if (e.shortKoreanName !== void 0) {
													let t = e.shortKoreanName, n = a;
													if (typeof t != "string" && t !== null) {
														let e = {};
														i === null ? i = [e] : i.push(e), a++;
													}
													var d = n === a;
												} else var d = !0;
												if (d) {
													if (e.suborg !== void 0) {
														let t = e.suborg, n = a;
														if (typeof t != "string" && t !== null) {
															let e = {};
															i === null ? i = [e] : i.push(e), a++;
														}
														var d = n === a;
													} else var d = !0;
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (w.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), w.errors = i, a === 0);
}
w.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var be = T, xe = {
	$id: "urn:hololive:admin:assertion:21d31947115359273d24dce3d6597bf50867fcb1b586d4b41fa83d9355f8c56a",
	not: { not: {
		type: "object",
		required: [
			"id",
			"name",
			"image",
			"status",
			"state",
			"created",
			"ports",
			"managed",
			"stopBlocked"
		],
		properties: {
			created: { type: "integer" },
			health: { type: ["string", "null"] },
			id: { type: "string" },
			image: { type: "string" },
			managed: { type: "boolean" },
			name: { type: "string" },
			ports: {
				type: "array",
				items: {
					type: "object",
					required: ["private_port", "port_type"],
					properties: {
						port_type: { type: "string" },
						private_port: {
							type: "integer",
							minimum: 0,
							maximum: 2147483647
						},
						public_port: {
							type: ["integer", "null"],
							minimum: 0,
							maximum: 2147483647
						}
					},
					additionalProperties: !1
				}
			},
			state: { type: "string" },
			status: { type: "string" },
			stopBlocked: { type: "boolean" }
		},
		additionalProperties: !1
	} }
}, Se = Object.prototype.hasOwnProperty;
function T(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = T.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.id === void 0 || e.name === void 0 || e.image === void 0 || e.status === void 0 || e.state === void 0 || e.created === void 0 || e.ports === void 0 || e.managed === void 0 || e.stopBlocked === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (!Se.call(xe.not.not.properties, t)) {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.created !== void 0) {
						let t = e.created, n = a;
						if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
						var d = n === a;
					} else var d = !0;
					if (d) {
						if (e.health !== void 0) {
							let t = e.health, n = a;
							if (typeof t != "string" && t !== null) {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
							var d = n === a;
						} else var d = !0;
						if (d) {
							if (e.id !== void 0) {
								let t = a;
								if (typeof e.id != "string") {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
								var d = t === a;
							} else var d = !0;
							if (d) {
								if (e.image !== void 0) {
									let t = a;
									if (typeof e.image != "string") {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
									var d = t === a;
								} else var d = !0;
								if (d) {
									if (e.managed !== void 0) {
										let t = a;
										if (typeof e.managed != "boolean") {
											let e = {};
											i === null ? i = [e] : i.push(e), a++;
										}
										var d = t === a;
									} else var d = !0;
									if (d) {
										if (e.name !== void 0) {
											let t = a;
											if (typeof e.name != "string") {
												let e = {};
												i === null ? i = [e] : i.push(e), a++;
											}
											var d = t === a;
										} else var d = !0;
										if (d) {
											if (e.ports !== void 0) {
												let t = e.ports, n = a;
												if (a === n) {
													if (Array.isArray(t)) {
														let e = t.length;
														for (let n = 0; n < e; n++) {
															let e = t[n], r = a;
															if (a === r) {
																if (e && typeof e == "object" && !Array.isArray(e)) {
																	if (e.private_port === void 0 || e.port_type === void 0) {
																		let e = {};
																		i === null ? i = [e] : i.push(e), a++;
																	} else {
																		let t = a;
																		for (let t in e) if (t !== "port_type" && t !== "private_port" && t !== "public_port") {
																			let e = {};
																			i === null ? i = [e] : i.push(e), a++;
																			break;
																		}
																		if (t === a) {
																			if (e.port_type !== void 0) {
																				let t = a;
																				if (typeof e.port_type != "string") {
																					let e = {};
																					i === null ? i = [e] : i.push(e), a++;
																				}
																				var f = t === a;
																			} else var f = !0;
																			if (f) {
																				if (e.private_port !== void 0) {
																					let t = e.private_port, n = a;
																					if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																						let e = {};
																						i === null ? i = [e] : i.push(e), a++;
																					}
																					if (a === n && typeof t == "number" && isFinite(t)) {
																						if (t > 2147483647 || isNaN(t)) {
																							let e = {};
																							i === null ? i = [e] : i.push(e), a++;
																						} else if (t < 0 || isNaN(t)) {
																							let e = {};
																							i === null ? i = [e] : i.push(e), a++;
																						}
																					}
																					var f = n === a;
																				} else var f = !0;
																				if (f) {
																					if (e.public_port !== void 0) {
																						let t = e.public_port, n = a;
																						if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t)) && t !== null) {
																							let e = {};
																							i === null ? i = [e] : i.push(e), a++;
																						}
																						if (a === n && typeof t == "number" && isFinite(t)) {
																							if (t > 2147483647 || isNaN(t)) {
																								let e = {};
																								i === null ? i = [e] : i.push(e), a++;
																							} else if (t < 0 || isNaN(t)) {
																								let e = {};
																								i === null ? i = [e] : i.push(e), a++;
																							}
																						}
																						var f = n === a;
																					} else var f = !0;
																				}
																			}
																		}
																	}
																} else {
																	let e = {};
																	i === null ? i = [e] : i.push(e), a++;
																}
															}
															if (r !== a) break;
														}
													} else {
														let e = {};
														i === null ? i = [e] : i.push(e), a++;
													}
												}
												var d = n === a;
											} else var d = !0;
											if (d) {
												if (e.state !== void 0) {
													let t = a;
													if (typeof e.state != "string") {
														let e = {};
														i === null ? i = [e] : i.push(e), a++;
													}
													var d = t === a;
												} else var d = !0;
												if (d) {
													if (e.status !== void 0) {
														let t = a;
														if (typeof e.status != "string") {
															let e = {};
															i === null ? i = [e] : i.push(e), a++;
														}
														var d = t === a;
													} else var d = !0;
													if (d) {
														if (e.stopBlocked !== void 0) {
															let t = a;
															if (typeof e.stopBlocked != "boolean") {
																let e = {};
																i === null ? i = [e] : i.push(e), a++;
															}
															var d = t === a;
														} else var d = !0;
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (T.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), T.errors = i, a === 0);
}
T.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var E = O, D = e().default;
function O(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = O.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.roomId === void 0 || e.channelId === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "channelId" && t !== "roomId") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.channelId !== void 0) {
						let t = e.channelId, n = a;
						if (a === n) {
							if (typeof t == "string") {
								if (D(t) < 1) {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
							} else {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
						}
						var d = n === a;
					} else var d = !0;
					if (d) {
						if (e.roomId !== void 0) {
							let t = e.roomId, n = a;
							if (a === n) {
								if (typeof t == "string") {
									if (D(t) < 1) {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
								} else {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
							}
							var d = n === a;
						} else var d = !0;
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (O.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), O.errors = i, a === 0);
}
O.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var k = A;
function A(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = A.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			let t = a;
			for (let t in e) if (t !== "idle") {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
				break;
			}
			if (t === a && e.idle !== void 0 && typeof e.idle != "boolean") {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (A.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), A.errors = i, a === 0);
}
A.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var j = M;
function M(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = M.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.username === void 0 || e.password === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "password" && t !== "username") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.password !== void 0) {
						let t = a;
						if (typeof e.password != "string") {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
						var d = t === a;
					} else var d = !0;
					if (d) {
						if (e.username !== void 0) {
							let t = a;
							if (typeof e.username != "string") {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
							var d = t === a;
						} else var d = !0;
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (M.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), M.errors = i, a === 0);
}
M.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var Ce = N, we = /* @__PURE__ */ RegExp("^[1-9][0-9]*$", "u");
function N(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = N.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.id === void 0 || e.channelId === void 0 || e.name === void 0 || e.aliases === void 0 || e.isGraduated === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "aliases" && t !== "channelId" && t !== "id" && t !== "isGraduated" && t !== "name" && t !== "nameJa" && t !== "nameKo") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.aliases !== void 0) {
						let t = e.aliases, n = a;
						if (a === n) {
							if (t && typeof t == "object" && !Array.isArray(t)) {
								if (t.ko === void 0 || t.ja === void 0) {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								} else {
									let e = a;
									for (let e in t) if (e !== "ja" && e !== "ko") {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
										break;
									}
									if (e === a) {
										if (t.ja !== void 0) {
											let e = t.ja, n = a;
											if (a === n) {
												if (Array.isArray(e)) {
													let t = e.length;
													for (let n = 0; n < t; n++) {
														let t = a;
														if (typeof e[n] != "string") {
															let e = {};
															i === null ? i = [e] : i.push(e), a++;
														}
														if (t !== a) break;
													}
												} else {
													let e = {};
													i === null ? i = [e] : i.push(e), a++;
												}
											}
											var d = n === a;
										} else var d = !0;
										if (d) {
											if (t.ko !== void 0) {
												let e = t.ko, n = a;
												if (a === n) {
													if (Array.isArray(e)) {
														let t = e.length;
														for (let n = 0; n < t; n++) {
															let t = a;
															if (typeof e[n] != "string") {
																let e = {};
																i === null ? i = [e] : i.push(e), a++;
															}
															if (t !== a) break;
														}
													} else {
														let e = {};
														i === null ? i = [e] : i.push(e), a++;
													}
												}
												var d = n === a;
											} else var d = !0;
										}
									}
								}
							} else {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
						}
						var f = n === a;
					} else var f = !0;
					if (f) {
						if (e.channelId !== void 0) {
							let t = a;
							if (typeof e.channelId != "string") {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
							var f = t === a;
						} else var f = !0;
						if (f) {
							if (e.id !== void 0) {
								let t = e.id, n = a;
								if (a === n) {
									if (typeof t == "string") {
										if (!we.test(t)) {
											let e = {};
											i === null ? i = [e] : i.push(e), a++;
										}
									} else {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
								}
								var f = n === a;
							} else var f = !0;
							if (f) {
								if (e.isGraduated !== void 0) {
									let t = a;
									if (typeof e.isGraduated != "boolean") {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
									var f = t === a;
								} else var f = !0;
								if (f) {
									if (e.name !== void 0) {
										let t = a;
										if (typeof e.name != "string") {
											let e = {};
											i === null ? i = [e] : i.push(e), a++;
										}
										var f = t === a;
									} else var f = !0;
									if (f) {
										if (e.nameJa !== void 0) {
											let t = e.nameJa, n = a;
											if (typeof t != "string" && t !== null) {
												let e = {};
												i === null ? i = [e] : i.push(e), a++;
											}
											var f = n === a;
										} else var f = !0;
										if (f) {
											if (e.nameKo !== void 0) {
												let t = e.nameKo, n = a;
												if (typeof t != "string" && t !== null) {
													let e = {};
													i === null ? i = [e] : i.push(e), a++;
												}
												var f = n === a;
											} else var f = !0;
										}
									}
								}
							}
						}
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (N.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), N.errors = i, a === 0);
}
N.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var Te = P;
function P(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = P.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.private_port === void 0 || e.port_type === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "port_type" && t !== "private_port" && t !== "public_port") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.port_type !== void 0) {
						let t = a;
						if (typeof e.port_type != "string") {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
						var d = t === a;
					} else var d = !0;
					if (d) {
						if (e.private_port !== void 0) {
							let t = e.private_port, n = a;
							if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
							if (a === n && typeof t == "number" && isFinite(t)) {
								if (t > 2147483647 || isNaN(t)) {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								} else if (t < 0 || isNaN(t)) {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
							}
							var d = n === a;
						} else var d = !0;
						if (d) {
							if (e.public_port !== void 0) {
								let t = e.public_port, n = a;
								if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t)) && t !== null) {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
								if (a === n && typeof t == "number" && isFinite(t)) {
									if (t > 2147483647 || isNaN(t)) {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									} else if (t < 0 || isNaN(t)) {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
								}
								var d = n === a;
							} else var d = !0;
						}
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (P.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), P.errors = i, a === 0);
}
P.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var F = I;
function I(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = I.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.type === void 0 || e.alias === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "alias" && t !== "type") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.alias !== void 0) {
						let t = a;
						if (typeof e.alias != "string") {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
						var d = t === a;
					} else var d = !0;
					if (d) {
						if (e.type !== void 0) {
							let t = a;
							if (typeof e.type != "string") {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
							var d = t === a;
						} else var d = !0;
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (I.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), I.errors = i, a === 0);
}
I.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var Ee = R, L = e().default;
function R(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = R.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.roomId === void 0 || e.roomName === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "roomId" && t !== "roomName") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.roomId !== void 0) {
						let t = e.roomId, n = a;
						if (a === n) {
							if (typeof t == "string") {
								if (L(t) < 1) {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
							} else {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
						}
						var d = n === a;
					} else var d = !0;
					if (d) {
						if (e.roomName !== void 0) {
							let t = e.roomName, n = a;
							if (a === n) {
								if (typeof t == "string") {
									if (L(t) < 1) {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
								} else {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
							}
							var d = n === a;
						} else var d = !0;
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (R.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), R.errors = i, a === 0);
}
R.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var De = z;
function z(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = z.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.chatId === void 0 || e.name === void 0 || e.type === void 0 || e.memberCount === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "chatId" && t !== "name" && t !== "type" && t !== "memberCount") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.chatId !== void 0) {
						let t = a;
						if (typeof e.chatId != "string") {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
						var d = t === a;
					} else var d = !0;
					if (d) {
						if (e.name !== void 0) {
							let t = a;
							if (typeof e.name != "string") {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
							var d = t === a;
						} else var d = !0;
						if (d) {
							if (e.type !== void 0) {
								let t = a;
								if (typeof e.type != "string") {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
								var d = t === a;
							} else var d = !0;
							if (d) {
								if (e.memberCount !== void 0) {
									let t = e.memberCount, n = a;
									if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
									var d = n === a;
								} else var d = !0;
							}
						}
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (z.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), z.errors = i, a === 0);
}
z.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var Oe = B;
function B(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = B.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.name === void 0 || e.available === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "available" && t !== "error" && t !== "name" && t !== "response_time_ms") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.available !== void 0) {
						let t = a;
						if (typeof e.available != "boolean") {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
						var d = t === a;
					} else var d = !0;
					if (d) {
						if (e.error !== void 0) {
							let t = e.error, n = a;
							if (typeof t != "string" && t !== null) {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
							var d = n === a;
						} else var d = !0;
						if (d) {
							if (e.name !== void 0) {
								let t = a;
								if (typeof e.name != "string") {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
								var d = t === a;
							} else var d = !0;
							if (d) {
								if (e.response_time_ms !== void 0) {
									let t = e.response_time_ms, n = a;
									if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t)) && t !== null) {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
									if (a === n && typeof t == "number" && isFinite(t) && (t < 0 || isNaN(t))) {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
									var d = n === a;
								} else var d = !0;
							}
						}
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (B.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), B.errors = i, a === 0);
}
B.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var ke = V;
function V(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = V.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.heartbeat_interval_ms === void 0 || e.idle_timeout_ms === void 0 || e.idle_warning_timeout_ms === void 0 || e.idle_session_ttl_ms === void 0 || e.absolute_warning_window_ms === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "absolute_warning_window_ms" && t !== "heartbeat_interval_ms" && t !== "idle_session_ttl_ms" && t !== "idle_timeout_ms" && t !== "idle_warning_timeout_ms") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.absolute_warning_window_ms !== void 0) {
						let t = e.absolute_warning_window_ms, n = a;
						if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
						if (a === n && typeof t == "number" && isFinite(t) && (t < 0 || isNaN(t))) {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
						var d = n === a;
					} else var d = !0;
					if (d) {
						if (e.heartbeat_interval_ms !== void 0) {
							let t = e.heartbeat_interval_ms, n = a;
							if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
							if (a === n && typeof t == "number" && isFinite(t) && (t < 0 || isNaN(t))) {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
							var d = n === a;
						} else var d = !0;
						if (d) {
							if (e.idle_session_ttl_ms !== void 0) {
								let t = e.idle_session_ttl_ms, n = a;
								if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
								if (a === n && typeof t == "number" && isFinite(t) && (t < 0 || isNaN(t))) {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
								var d = n === a;
							} else var d = !0;
							if (d) {
								if (e.idle_timeout_ms !== void 0) {
									let t = e.idle_timeout_ms, n = a;
									if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
									if (a === n && typeof t == "number" && isFinite(t) && (t < 0 || isNaN(t))) {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
									var d = n === a;
								} else var d = !0;
								if (d) {
									if (e.idle_warning_timeout_ms !== void 0) {
										let t = e.idle_warning_timeout_ms, n = a;
										if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
											let e = {};
											i === null ? i = [e] : i.push(e), a++;
										}
										if (a === n && typeof t == "number" && isFinite(t) && (t < 0 || isNaN(t))) {
											let e = {};
											i === null ? i = [e] : i.push(e), a++;
										}
										var d = n === a;
									} else var d = !0;
								}
							}
						}
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (V.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), V.errors = i, a === 0);
}
V.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var Ae = H;
function H(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = H.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			let t = a;
			for (let t in e) if (t !== "enabled" && t !== "mode") {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
				break;
			}
			if (t === a) {
				if (e.enabled !== void 0) {
					let t = e.enabled, n = a;
					if (typeof t != "boolean" && t !== null) {
						let e = {};
						i === null ? i = [e] : i.push(e), a++;
					}
					var d = n === a;
				} else var d = !0;
				if (d) {
					if (e.mode !== void 0) {
						let t = e.mode, n = a;
						if (typeof t != "string" && t !== null) {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
						var d = n === a;
					} else var d = !0;
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (H.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), H.errors = i, a === 0);
}
H.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var je = U;
function U(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = U.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.isGraduated === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "isGraduated") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a && e.isGraduated !== void 0 && typeof e.isGraduated != "boolean") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (U.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), U.errors = i, a === 0);
}
U.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var Me = W;
function W(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = W.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.alarmAdvanceMinutes === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "alarmAdvanceMinutes") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a && e.alarmAdvanceMinutes !== void 0) {
					let t = e.alarmAdvanceMinutes, n = a;
					if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
						let e = {};
						i === null ? i = [e] : i.push(e), a++;
					}
					if (a === n && typeof t == "number" && isFinite(t)) {
						if (t > 1440 || isNaN(t)) {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						} else if (t < 0 || isNaN(t)) {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (W.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), W.errors = i, a === 0);
}
W.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var Ne = G, Pe = {
	$id: "urn:hololive:admin:assertion:02240ea583b01525269b19c596e62593433dfbb38a40a4db9f04c514efb718d0",
	not: { not: {
		type: "object",
		required: [
			"id",
			"title",
			"status",
			"channel_id"
		],
		properties: {
			channel_id: { type: "string" },
			channel_name: { type: ["string", "null"] },
			id: { type: "string" },
			link: { type: ["string", "null"] },
			start_actual: { type: ["string", "null"] },
			start_scheduled: { type: ["string", "null"] },
			status: { type: "string" },
			thumbnail: { type: ["string", "null"] },
			title: { type: "string" }
		},
		additionalProperties: !1
	} }
}, Fe = Object.prototype.hasOwnProperty;
function G(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = G.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.id === void 0 || e.title === void 0 || e.status === void 0 || e.channel_id === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (!Fe.call(Pe.not.not.properties, t)) {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.channel_id !== void 0) {
						let t = a;
						if (typeof e.channel_id != "string") {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
						var d = t === a;
					} else var d = !0;
					if (d) {
						if (e.channel_name !== void 0) {
							let t = e.channel_name, n = a;
							if (typeof t != "string" && t !== null) {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
							var d = n === a;
						} else var d = !0;
						if (d) {
							if (e.id !== void 0) {
								let t = a;
								if (typeof e.id != "string") {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
								var d = t === a;
							} else var d = !0;
							if (d) {
								if (e.link !== void 0) {
									let t = e.link, n = a;
									if (typeof t != "string" && t !== null) {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
									var d = n === a;
								} else var d = !0;
								if (d) {
									if (e.start_actual !== void 0) {
										let t = e.start_actual, n = a;
										if (typeof t != "string" && t !== null) {
											let e = {};
											i === null ? i = [e] : i.push(e), a++;
										}
										var d = n === a;
									} else var d = !0;
									if (d) {
										if (e.start_scheduled !== void 0) {
											let t = e.start_scheduled, n = a;
											if (typeof t != "string" && t !== null) {
												let e = {};
												i === null ? i = [e] : i.push(e), a++;
											}
											var d = n === a;
										} else var d = !0;
										if (d) {
											if (e.status !== void 0) {
												let t = a;
												if (typeof e.status != "string") {
													let e = {};
													i === null ? i = [e] : i.push(e), a++;
												}
												var d = t === a;
											} else var d = !0;
											if (d) {
												if (e.thumbnail !== void 0) {
													let t = e.thumbnail, n = a;
													if (typeof t != "string" && t !== null) {
														let e = {};
														i === null ? i = [e] : i.push(e), a++;
													}
													var d = n === a;
												} else var d = !0;
												if (d) {
													if (e.title !== void 0) {
														let t = a;
														if (typeof e.title != "string") {
															let e = {};
															i === null ? i = [e] : i.push(e), a++;
														}
														var d = t === a;
													} else var d = !0;
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (G.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), G.errors = i, a === 0);
}
G.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var Ie = K, Le = e().default;
function K(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = K.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.channelId === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "channelId") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a && e.channelId !== void 0) {
					let t = e.channelId;
					if (a === a) {
						if (typeof t == "string") {
							if (Le(t) < 1) {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
						} else {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (K.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), K.errors = i, a === 0);
}
K.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var Re = q, ze = e().default;
function q(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = q.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.name === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "name") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a && e.name !== void 0) {
					let t = e.name;
					if (a === a) {
						if (typeof t == "string") {
							if (ze(t) < 1) {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
						} else {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (q.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), q.errors = i, a === 0);
}
q.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var Be = Y, J = e().default;
function Y(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = Y.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.userId === void 0 || e.userName === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "userId" && t !== "userName") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.userId !== void 0) {
						let t = e.userId, n = a;
						if (a === n) {
							if (typeof t == "string") {
								if (J(t) < 1) {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
							} else {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
						}
						var d = n === a;
					} else var d = !0;
					if (d) {
						if (e.userName !== void 0) {
							let t = e.userName, n = a;
							if (a === n) {
								if (typeof t == "string") {
									if (J(t) < 1) {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
								} else {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
							}
							var d = n === a;
						} else var d = !0;
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (Y.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), Y.errors = i, a === 0);
}
Y.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var Ve = X, He = {
	$id: "urn:hololive:admin:assertion:e21cc39d91bade6efd64fca03cec8127b85e71ed45c912d97f91f8328f3d03a2",
	not: { not: {
		type: "object",
		required: [
			"channelId",
			"detectedPostCount",
			"alarmSentPostCount",
			"successPostCount",
			"failedPostCount",
			"detectedUnsentPostCount",
			"pendingPostCount",
			"latencyMeasuredPostCount",
			"withinTargetPostCount",
			"exceededPostCount",
			"communityPostCount",
			"shortsPostCount"
		],
		properties: {
			alarmSentPostCount: { type: "integer" },
			averageLatencyMillis: { type: ["integer", "null"] },
			channelId: { type: "string" },
			communityPostCount: { type: "integer" },
			detectedPostCount: { type: "integer" },
			detectedUnsentPostCount: { type: "integer" },
			earliestObservedAt: { type: ["string", "null"] },
			exceededPostCount: { type: "integer" },
			failedPostCount: { type: "integer" },
			latencyMeasuredPostCount: { type: "integer" },
			latestObservedAt: { type: ["string", "null"] },
			maxLatencyMillis: { type: ["integer", "null"] },
			memberName: { type: ["string", "null"] },
			pendingPostCount: { type: "integer" },
			shortsPostCount: { type: "integer" },
			successPostCount: { type: "integer" },
			withinTargetPostCount: { type: "integer" }
		},
		additionalProperties: !1
	} }
}, Ue = Object.prototype.hasOwnProperty;
function X(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = X.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.channelId === void 0 || e.detectedPostCount === void 0 || e.alarmSentPostCount === void 0 || e.successPostCount === void 0 || e.failedPostCount === void 0 || e.detectedUnsentPostCount === void 0 || e.pendingPostCount === void 0 || e.latencyMeasuredPostCount === void 0 || e.withinTargetPostCount === void 0 || e.exceededPostCount === void 0 || e.communityPostCount === void 0 || e.shortsPostCount === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (!Ue.call(He.not.not.properties, t)) {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.alarmSentPostCount !== void 0) {
						let t = e.alarmSentPostCount, n = a;
						if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
						var d = n === a;
					} else var d = !0;
					if (d) {
						if (e.averageLatencyMillis !== void 0) {
							let t = e.averageLatencyMillis, n = a;
							if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t)) && t !== null) {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
							var d = n === a;
						} else var d = !0;
						if (d) {
							if (e.channelId !== void 0) {
								let t = a;
								if (typeof e.channelId != "string") {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
								var d = t === a;
							} else var d = !0;
							if (d) {
								if (e.communityPostCount !== void 0) {
									let t = e.communityPostCount, n = a;
									if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
									var d = n === a;
								} else var d = !0;
								if (d) {
									if (e.detectedPostCount !== void 0) {
										let t = e.detectedPostCount, n = a;
										if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
											let e = {};
											i === null ? i = [e] : i.push(e), a++;
										}
										var d = n === a;
									} else var d = !0;
									if (d) {
										if (e.detectedUnsentPostCount !== void 0) {
											let t = e.detectedUnsentPostCount, n = a;
											if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
												let e = {};
												i === null ? i = [e] : i.push(e), a++;
											}
											var d = n === a;
										} else var d = !0;
										if (d) {
											if (e.earliestObservedAt !== void 0) {
												let t = e.earliestObservedAt, n = a;
												if (typeof t != "string" && t !== null) {
													let e = {};
													i === null ? i = [e] : i.push(e), a++;
												}
												var d = n === a;
											} else var d = !0;
											if (d) {
												if (e.exceededPostCount !== void 0) {
													let t = e.exceededPostCount, n = a;
													if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
														let e = {};
														i === null ? i = [e] : i.push(e), a++;
													}
													var d = n === a;
												} else var d = !0;
												if (d) {
													if (e.failedPostCount !== void 0) {
														let t = e.failedPostCount, n = a;
														if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
															let e = {};
															i === null ? i = [e] : i.push(e), a++;
														}
														var d = n === a;
													} else var d = !0;
													if (d) {
														if (e.latencyMeasuredPostCount !== void 0) {
															let t = e.latencyMeasuredPostCount, n = a;
															if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																let e = {};
																i === null ? i = [e] : i.push(e), a++;
															}
															var d = n === a;
														} else var d = !0;
														if (d) {
															if (e.latestObservedAt !== void 0) {
																let t = e.latestObservedAt, n = a;
																if (typeof t != "string" && t !== null) {
																	let e = {};
																	i === null ? i = [e] : i.push(e), a++;
																}
																var d = n === a;
															} else var d = !0;
															if (d) {
																if (e.maxLatencyMillis !== void 0) {
																	let t = e.maxLatencyMillis, n = a;
																	if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t)) && t !== null) {
																		let e = {};
																		i === null ? i = [e] : i.push(e), a++;
																	}
																	var d = n === a;
																} else var d = !0;
																if (d) {
																	if (e.memberName !== void 0) {
																		let t = e.memberName, n = a;
																		if (typeof t != "string" && t !== null) {
																			let e = {};
																			i === null ? i = [e] : i.push(e), a++;
																		}
																		var d = n === a;
																	} else var d = !0;
																	if (d) {
																		if (e.pendingPostCount !== void 0) {
																			let t = e.pendingPostCount, n = a;
																			if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																				let e = {};
																				i === null ? i = [e] : i.push(e), a++;
																			}
																			var d = n === a;
																		} else var d = !0;
																		if (d) {
																			if (e.shortsPostCount !== void 0) {
																				let t = e.shortsPostCount, n = a;
																				if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																					let e = {};
																					i === null ? i = [e] : i.push(e), a++;
																				}
																				var d = n === a;
																			} else var d = !0;
																			if (d) {
																				if (e.successPostCount !== void 0) {
																					let t = e.successPostCount, n = a;
																					if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																						let e = {};
																						i === null ? i = [e] : i.push(e), a++;
																					}
																					var d = n === a;
																				} else var d = !0;
																				if (d) {
																					if (e.withinTargetPostCount !== void 0) {
																						let t = e.withinTargetPostCount, n = a;
																						if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																							let e = {};
																							i === null ? i = [e] : i.push(e), a++;
																						}
																						var d = n === a;
																					} else var d = !0;
																				}
																			}
																		}
																	}
																}
															}
														}
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (X.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), X.errors = i, a === 0);
}
X.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var We = Z, Ge = {
	$id: "urn:hololive:admin:assertion:797e35b7c66cdf7d855bee2d587473ee8d79e0f02e6b8d35af4ee4dedf04dbe5",
	not: { not: {
		type: "object",
		required: [
			"channelCount",
			"detectedPostCount",
			"alarmSentPostCount",
			"successPostCount",
			"failedPostCount",
			"detectedUnsentPostCount",
			"pendingPostCount",
			"latencyMeasuredPostCount",
			"withinTargetPostCount",
			"exceededPostCount",
			"communityDetectedPostCount",
			"shortsDetectedPostCount",
			"communityExceededPostCount",
			"shortsExceededPostCount"
		],
		properties: {
			alarmSentPostCount: { type: "integer" },
			averageLatencyMillis: { type: ["integer", "null"] },
			channelCount: { type: "integer" },
			communityDetectedPostCount: { type: "integer" },
			communityExceededPostCount: { type: "integer" },
			detectedPostCount: { type: "integer" },
			detectedUnsentPostCount: { type: "integer" },
			exceededPostCount: { type: "integer" },
			failedPostCount: { type: "integer" },
			latencyMeasuredPostCount: { type: "integer" },
			maxLatencyMillis: { type: ["integer", "null"] },
			pendingPostCount: { type: "integer" },
			shortsDetectedPostCount: { type: "integer" },
			shortsExceededPostCount: { type: "integer" },
			successPostCount: { type: "integer" },
			withinTargetPostCount: { type: "integer" }
		},
		additionalProperties: !1
	} }
}, Ke = Object.prototype.hasOwnProperty;
function Z(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = Z.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.channelCount === void 0 || e.detectedPostCount === void 0 || e.alarmSentPostCount === void 0 || e.successPostCount === void 0 || e.failedPostCount === void 0 || e.detectedUnsentPostCount === void 0 || e.pendingPostCount === void 0 || e.latencyMeasuredPostCount === void 0 || e.withinTargetPostCount === void 0 || e.exceededPostCount === void 0 || e.communityDetectedPostCount === void 0 || e.shortsDetectedPostCount === void 0 || e.communityExceededPostCount === void 0 || e.shortsExceededPostCount === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (!Ke.call(Ge.not.not.properties, t)) {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.alarmSentPostCount !== void 0) {
						let t = e.alarmSentPostCount, n = a;
						if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
						var d = n === a;
					} else var d = !0;
					if (d) {
						if (e.averageLatencyMillis !== void 0) {
							let t = e.averageLatencyMillis, n = a;
							if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t)) && t !== null) {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
							var d = n === a;
						} else var d = !0;
						if (d) {
							if (e.channelCount !== void 0) {
								let t = e.channelCount, n = a;
								if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
								var d = n === a;
							} else var d = !0;
							if (d) {
								if (e.communityDetectedPostCount !== void 0) {
									let t = e.communityDetectedPostCount, n = a;
									if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
									var d = n === a;
								} else var d = !0;
								if (d) {
									if (e.communityExceededPostCount !== void 0) {
										let t = e.communityExceededPostCount, n = a;
										if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
											let e = {};
											i === null ? i = [e] : i.push(e), a++;
										}
										var d = n === a;
									} else var d = !0;
									if (d) {
										if (e.detectedPostCount !== void 0) {
											let t = e.detectedPostCount, n = a;
											if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
												let e = {};
												i === null ? i = [e] : i.push(e), a++;
											}
											var d = n === a;
										} else var d = !0;
										if (d) {
											if (e.detectedUnsentPostCount !== void 0) {
												let t = e.detectedUnsentPostCount, n = a;
												if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
													let e = {};
													i === null ? i = [e] : i.push(e), a++;
												}
												var d = n === a;
											} else var d = !0;
											if (d) {
												if (e.exceededPostCount !== void 0) {
													let t = e.exceededPostCount, n = a;
													if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
														let e = {};
														i === null ? i = [e] : i.push(e), a++;
													}
													var d = n === a;
												} else var d = !0;
												if (d) {
													if (e.failedPostCount !== void 0) {
														let t = e.failedPostCount, n = a;
														if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
															let e = {};
															i === null ? i = [e] : i.push(e), a++;
														}
														var d = n === a;
													} else var d = !0;
													if (d) {
														if (e.latencyMeasuredPostCount !== void 0) {
															let t = e.latencyMeasuredPostCount, n = a;
															if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																let e = {};
																i === null ? i = [e] : i.push(e), a++;
															}
															var d = n === a;
														} else var d = !0;
														if (d) {
															if (e.maxLatencyMillis !== void 0) {
																let t = e.maxLatencyMillis, n = a;
																if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t)) && t !== null) {
																	let e = {};
																	i === null ? i = [e] : i.push(e), a++;
																}
																var d = n === a;
															} else var d = !0;
															if (d) {
																if (e.pendingPostCount !== void 0) {
																	let t = e.pendingPostCount, n = a;
																	if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																		let e = {};
																		i === null ? i = [e] : i.push(e), a++;
																	}
																	var d = n === a;
																} else var d = !0;
																if (d) {
																	if (e.shortsDetectedPostCount !== void 0) {
																		let t = e.shortsDetectedPostCount, n = a;
																		if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																			let e = {};
																			i === null ? i = [e] : i.push(e), a++;
																		}
																		var d = n === a;
																	} else var d = !0;
																	if (d) {
																		if (e.shortsExceededPostCount !== void 0) {
																			let t = e.shortsExceededPostCount, n = a;
																			if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																				let e = {};
																				i === null ? i = [e] : i.push(e), a++;
																			}
																			var d = n === a;
																		} else var d = !0;
																		if (d) {
																			if (e.successPostCount !== void 0) {
																				let t = e.successPostCount, n = a;
																				if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																					let e = {};
																					i === null ? i = [e] : i.push(e), a++;
																				}
																				var d = n === a;
																			} else var d = !0;
																			if (d) {
																				if (e.withinTargetPostCount !== void 0) {
																					let t = e.withinTargetPostCount, n = a;
																					if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																						let e = {};
																						i === null ? i = [e] : i.push(e), a++;
																					}
																					var d = n === a;
																				} else var d = !0;
																			}
																		}
																	}
																}
															}
														}
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (Z.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), Z.errors = i, a === 0);
}
Z.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var qe = Q;
function Q(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = Q.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.alarm_applied !== void 0) {
				let t = a;
				if (typeof e.alarm_applied != "boolean") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
				}
				var d = t === a;
			} else var d = !0;
			if (d) {
				if (e.alarm_requested_advance_minutes !== void 0) {
					let t = e.alarm_requested_advance_minutes, n = a;
					if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
						let e = {};
						i === null ? i = [e] : i.push(e), a++;
					}
					var d = n === a;
				} else var d = !0;
				if (d) {
					if (e.alarm_reason !== void 0) {
						let t = a;
						if (typeof e.alarm_reason != "string") {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
						var d = t === a;
					} else var d = !0;
					if (d) {
						if (e.alarm_target_minutes !== void 0) {
							let t = e.alarm_target_minutes, n = a;
							if (a === n) {
								if (Array.isArray(t)) {
									let e = t.length;
									for (let n = 0; n < e; n++) {
										let e = t[n], r = a;
										if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
											let e = {};
											i === null ? i = [e] : i.push(e), a++;
										}
										if (r !== a) break;
									}
								} else {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
							}
							var d = n === a;
						} else var d = !0;
						if (d) {
							if (e.config_publish_alarm_advance_minutes !== void 0) {
								let t = a;
								if (typeof e.config_publish_alarm_advance_minutes != "boolean") {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
								var d = t === a;
							} else var d = !0;
							if (d) {
								if (e.config_publish_alarm_advance_minutes_error !== void 0) {
									let t = a;
									if (typeof e.config_publish_alarm_advance_minutes_error != "string") {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
									var d = t === a;
								} else var d = !0;
							}
						}
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? (Q.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), Q.errors = i, a === 0);
}
Q.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var Je = $;
function $(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: ee = e, dynamicAnchors: te = {} } = {}) {
	let i = null, a = 0, o = $.evaluated;
	o.dynamicProps && (o.props = void 0), o.dynamicItems && (o.items = void 0);
	let s = a, c = a, l = a, u = a;
	if (a === u) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.name === void 0 || e.count === void 0 || e.metricKind === void 0 || e.available === void 0) {
				let e = {};
				i === null ? i = [e] : i.push(e), a++;
			} else {
				let t = a;
				for (let t in e) if (t !== "name" && t !== "count" && t !== "metricKind" && t !== "available" && t !== "error") {
					let e = {};
					i === null ? i = [e] : i.push(e), a++;
					break;
				}
				if (t === a) {
					if (e.name !== void 0) {
						let t = a;
						if (typeof e.name != "string") {
							let e = {};
							i === null ? i = [e] : i.push(e), a++;
						}
						var d = t === a;
					} else var d = !0;
					if (d) {
						if (e.count !== void 0) {
							let t = e.count, n = a;
							if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
							if (a === n && typeof t == "number" && isFinite(t) && (t < 0 || isNaN(t))) {
								let e = {};
								i === null ? i = [e] : i.push(e), a++;
							}
							var d = n === a;
						} else var d = !0;
						if (d) {
							if (e.metricKind !== void 0) {
								let t = e.metricKind, n = a;
								if (typeof t != "string") {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
								if (t !== "goroutine" && t !== "thread") {
									let e = {};
									i === null ? i = [e] : i.push(e), a++;
								}
								var d = n === a;
							} else var d = !0;
							if (d) {
								if (e.available !== void 0) {
									let t = a;
									if (typeof e.available != "boolean") {
										let e = {};
										i === null ? i = [e] : i.push(e), a++;
									}
									var d = t === a;
								} else var d = !0;
								if (d) {
									if (e.error !== void 0) {
										let t = e.error, n = a;
										if (typeof t != "string" && t !== null) {
											let e = {};
											i === null ? i = [e] : i.push(e), a++;
										}
										var d = n === a;
									} else var d = !0;
								}
							}
						}
					}
				}
			}
		} else {
			let e = {};
			i === null ? i = [e] : i.push(e), a++;
		}
	}
	if (u === a) {
		let e = {};
		i === null ? i = [e] : i.push(e), a++;
	} else a = l, i !== null && (l ? i.length = l : i = null);
	return c === a ? ($.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (a = s, i !== null && (s ? i.length = s : i = null), $.errors = i, a === 0);
}
$.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
export { h as AddAliasRequest, _ as AddMemberRequest, y as AddRoomRequest, y as RemoveRoomRequest, ce as AdminMetadata, ie as AggregatedStatus, ue as Alarm, o as AlarmsResponse, de as Aliases, fe as CalendarEntry, ge as CalendarMember, l as CalendarResponse, be as Container, E as DeleteAlarmRequest, s as DeleteAlarmResponse, i as DockerContainerListResponse, a as DockerHealthResponse, t as ErrorResponse, k as HeartbeatRequest, n as HeartbeatResponse, De as JoinedRoom, u as JoinedRoomsResponse, j as LoginRequest, r as LoginResponse, Ce as Member, c as MembersResponse, oe as OpenAPIDocument, Te as PortMapping, F as RemoveAliasRequest, Ee as RoomNameUpdateRequest, d as RoomsResponse, Je as ServiceRuntimeStats, Oe as ServiceStatus, ke as SessionPolicyResponse, te as SessionStatusResponse, Ae as SetAclRequest, f as SetAclResponse, je as SetGraduationRequest, Me as Settings, p as SettingsResponse, qe as SettingsRuntimeResult, ne as SettingsUpdateResponse, re as StatsResponse, ee as StatusOnlyResponse, Ne as Stream, se as StreamsResponse, m as SystemStats, Ie as UpdateChannelRequest, Re as UpdateMemberNameRequest, Be as UserNameUpdateRequest, Ve as YouTubeCommunityShortsOpsChannel, We as YouTubeCommunityShortsOpsOverview, ae as YouTubeCommunityShortsOpsResponse };
