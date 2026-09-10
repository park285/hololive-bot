var e = n, t = /* @__PURE__ */ RegExp("^[1-9][0-9]*$", "u");
function n(e, { instancePath: r = "", parentData: i, parentDataProperty: a, rootData: o = e, dynamicAnchors: s = {} } = {}) {
	let c = null, l = 0, u = n.evaluated;
	u.dynamicProps && (u.props = void 0), u.dynamicItems && (u.items = void 0);
	let d = l, f = l, p = l, m = l;
	if (l === m) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.status === void 0 || e.members === void 0) {
				let e = {};
				c === null ? c = [e] : c.push(e), l++;
			} else {
				let n = l;
				for (let t in e) if (t !== "members" && t !== "status") {
					let e = {};
					c === null ? c = [e] : c.push(e), l++;
					break;
				}
				if (n === l) {
					if (e.members !== void 0) {
						let n = e.members, r = l;
						if (l === r) {
							if (Array.isArray(n)) {
								let e = n.length;
								for (let r = 0; r < e; r++) {
									let e = n[r], i = l;
									if (l === i) {
										if (e && typeof e == "object" && !Array.isArray(e)) {
											if (e.id === void 0 || e.channelId === void 0 || e.name === void 0 || e.aliases === void 0 || e.isGraduated === void 0) {
												let e = {};
												c === null ? c = [e] : c.push(e), l++;
											} else {
												let n = l;
												for (let t in e) if (t !== "aliases" && t !== "channelId" && t !== "id" && t !== "isGraduated" && t !== "name" && t !== "nameJa" && t !== "nameKo") {
													let e = {};
													c === null ? c = [e] : c.push(e), l++;
													break;
												}
												if (n === l) {
													if (e.aliases !== void 0) {
														let t = e.aliases, n = l;
														if (l === n) {
															if (t && typeof t == "object" && !Array.isArray(t)) {
																if (t.ko === void 0 || t.ja === void 0) {
																	let e = {};
																	c === null ? c = [e] : c.push(e), l++;
																} else {
																	let e = l;
																	for (let e in t) if (e !== "ja" && e !== "ko") {
																		let e = {};
																		c === null ? c = [e] : c.push(e), l++;
																		break;
																	}
																	if (e === l) {
																		if (t.ja !== void 0) {
																			let e = t.ja, n = l;
																			if (l === n) {
																				if (Array.isArray(e)) {
																					let t = e.length;
																					for (let n = 0; n < t; n++) {
																						let t = l;
																						if (typeof e[n] != "string") {
																							let e = {};
																							c === null ? c = [e] : c.push(e), l++;
																						}
																						if (t !== l) break;
																					}
																				} else {
																					let e = {};
																					c === null ? c = [e] : c.push(e), l++;
																				}
																			}
																			var h = n === l;
																		} else var h = !0;
																		if (h) {
																			if (t.ko !== void 0) {
																				let e = t.ko, n = l;
																				if (l === n) {
																					if (Array.isArray(e)) {
																						let t = e.length;
																						for (let n = 0; n < t; n++) {
																							let t = l;
																							if (typeof e[n] != "string") {
																								let e = {};
																								c === null ? c = [e] : c.push(e), l++;
																							}
																							if (t !== l) break;
																						}
																					} else {
																						let e = {};
																						c === null ? c = [e] : c.push(e), l++;
																					}
																				}
																				var h = n === l;
																			} else var h = !0;
																		}
																	}
																}
															} else {
																let e = {};
																c === null ? c = [e] : c.push(e), l++;
															}
														}
														var g = n === l;
													} else var g = !0;
													if (g) {
														if (e.channelId !== void 0) {
															let t = l;
															if (typeof e.channelId != "string") {
																let e = {};
																c === null ? c = [e] : c.push(e), l++;
															}
															var g = t === l;
														} else var g = !0;
														if (g) {
															if (e.id !== void 0) {
																let n = e.id, r = l;
																if (l === r) {
																	if (typeof n == "string") {
																		if (!t.test(n)) {
																			let e = {};
																			c === null ? c = [e] : c.push(e), l++;
																		}
																	} else {
																		let e = {};
																		c === null ? c = [e] : c.push(e), l++;
																	}
																}
																var g = r === l;
															} else var g = !0;
															if (g) {
																if (e.isGraduated !== void 0) {
																	let t = l;
																	if (typeof e.isGraduated != "boolean") {
																		let e = {};
																		c === null ? c = [e] : c.push(e), l++;
																	}
																	var g = t === l;
																} else var g = !0;
																if (g) {
																	if (e.name !== void 0) {
																		let t = l;
																		if (typeof e.name != "string") {
																			let e = {};
																			c === null ? c = [e] : c.push(e), l++;
																		}
																		var g = t === l;
																	} else var g = !0;
																	if (g) {
																		if (e.nameJa !== void 0) {
																			let t = e.nameJa, n = l;
																			if (typeof t != "string" && t !== null) {
																				let e = {};
																				c === null ? c = [e] : c.push(e), l++;
																			}
																			var g = n === l;
																		} else var g = !0;
																		if (g) {
																			if (e.nameKo !== void 0) {
																				let t = e.nameKo, n = l;
																				if (typeof t != "string" && t !== null) {
																					let e = {};
																					c === null ? c = [e] : c.push(e), l++;
																				}
																				var g = n === l;
																			} else var g = !0;
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
											c === null ? c = [e] : c.push(e), l++;
										}
									}
									if (i !== l) break;
								}
							} else {
								let e = {};
								c === null ? c = [e] : c.push(e), l++;
							}
						}
						var _ = r === l;
					} else var _ = !0;
					if (_) {
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
							var _ = n === l;
						} else var _ = !0;
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
	return f === l ? (n.errors = [{
		instancePath: r,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (l = d, c !== null && (d ? c.length = d : c = null), n.errors = c, l === 0);
}
n.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
export { e as t };
