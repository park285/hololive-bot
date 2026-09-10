var e = t;
function t(e, { instancePath: n = "", parentData: r, parentDataProperty: i, rootData: a = e, dynamicAnchors: o = {} } = {}) {
	let s = null, c = 0, l = t.evaluated;
	l.dynamicProps && (l.props = void 0), l.dynamicItems && (l.items = void 0);
	let u = c, d = c, f = c, p = c;
	if (c === p) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.status === void 0 || e.rooms === void 0 || e.aclEnabled === void 0 || e.aclMode === void 0) {
				let e = {};
				s === null ? s = [e] : s.push(e), c++;
			} else {
				let t = c;
				for (let t in e) if (t !== "aclEnabled" && t !== "aclMode" && t !== "rooms" && t !== "status") {
					let e = {};
					s === null ? s = [e] : s.push(e), c++;
					break;
				}
				if (t === c) {
					if (e.aclEnabled !== void 0) {
						let t = c;
						if (typeof e.aclEnabled != "boolean") {
							let e = {};
							s === null ? s = [e] : s.push(e), c++;
						}
						var m = t === c;
					} else var m = !0;
					if (m) {
						if (e.aclMode !== void 0) {
							let t = e.aclMode, n = c;
							if (typeof t != "string") {
								let e = {};
								s === null ? s = [e] : s.push(e), c++;
							}
							if (t !== "whitelist" && t !== "blacklist") {
								let e = {};
								s === null ? s = [e] : s.push(e), c++;
							}
							var m = n === c;
						} else var m = !0;
						if (m) {
							if (e.rooms !== void 0) {
								let t = e.rooms, n = c;
								if (c === n) {
									if (Array.isArray(t)) {
										let e = t.length;
										for (let n = 0; n < e; n++) {
											let e = c;
											if (typeof t[n] != "string") {
												let e = {};
												s === null ? s = [e] : s.push(e), c++;
											}
											if (e !== c) break;
										}
									} else {
										let e = {};
										s === null ? s = [e] : s.push(e), c++;
									}
								}
								var m = n === c;
							} else var m = !0;
							if (m) {
								if (e.status !== void 0) {
									let t = e.status, n = c;
									if (typeof t != "string") {
										let e = {};
										s === null ? s = [e] : s.push(e), c++;
									}
									if (t !== "ok") {
										let e = {};
										s === null ? s = [e] : s.push(e), c++;
									}
									var m = n === c;
								} else var m = !0;
							}
						}
					}
				}
			}
		} else {
			let e = {};
			s === null ? s = [e] : s.push(e), c++;
		}
	}
	if (p === c) {
		let e = {};
		s === null ? s = [e] : s.push(e), c++;
	} else c = f, s !== null && (f ? s.length = f : s = null);
	return d === c ? (t.errors = [{
		instancePath: n,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (c = u, s !== null && (u ? s.length = u : s = null), t.errors = s, c === 0);
}
t.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var n = r;
function r(e, { instancePath: t = "", parentData: n, parentDataProperty: i, rootData: a = e, dynamicAnchors: o = {} } = {}) {
	let s = null, c = 0, l = r.evaluated;
	l.dynamicProps && (l.props = void 0), l.dynamicItems && (l.items = void 0);
	let u = c, d = c, f = c, p = c;
	if (c === p) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.status === void 0 || e.rooms === void 0) {
				let e = {};
				s === null ? s = [e] : s.push(e), c++;
			} else {
				let t = c;
				for (let t in e) if (t !== "status" && t !== "rooms") {
					let e = {};
					s === null ? s = [e] : s.push(e), c++;
					break;
				}
				if (t === c) {
					if (e.status !== void 0) {
						let t = e.status, n = c;
						if (typeof t != "string") {
							let e = {};
							s === null ? s = [e] : s.push(e), c++;
						}
						if (t !== "ok") {
							let e = {};
							s === null ? s = [e] : s.push(e), c++;
						}
						var m = n === c;
					} else var m = !0;
					if (m) {
						if (e.rooms !== void 0) {
							let t = e.rooms, n = c;
							if (c === n) {
								if (Array.isArray(t)) {
									let e = t.length;
									for (let n = 0; n < e; n++) {
										let e = t[n], r = c;
										if (c === r) {
											if (e && typeof e == "object" && !Array.isArray(e)) {
												if (e.chatId === void 0 || e.name === void 0 || e.type === void 0 || e.memberCount === void 0) {
													let e = {};
													s === null ? s = [e] : s.push(e), c++;
												} else {
													let t = c;
													for (let t in e) if (t !== "chatId" && t !== "name" && t !== "type" && t !== "memberCount") {
														let e = {};
														s === null ? s = [e] : s.push(e), c++;
														break;
													}
													if (t === c) {
														if (e.chatId !== void 0) {
															let t = c;
															if (typeof e.chatId != "string") {
																let e = {};
																s === null ? s = [e] : s.push(e), c++;
															}
															var h = t === c;
														} else var h = !0;
														if (h) {
															if (e.name !== void 0) {
																let t = c;
																if (typeof e.name != "string") {
																	let e = {};
																	s === null ? s = [e] : s.push(e), c++;
																}
																var h = t === c;
															} else var h = !0;
															if (h) {
																if (e.type !== void 0) {
																	let t = c;
																	if (typeof e.type != "string") {
																		let e = {};
																		s === null ? s = [e] : s.push(e), c++;
																	}
																	var h = t === c;
																} else var h = !0;
																if (h) {
																	if (e.memberCount !== void 0) {
																		let t = e.memberCount, n = c;
																		if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																			let e = {};
																			s === null ? s = [e] : s.push(e), c++;
																		}
																		var h = n === c;
																	} else var h = !0;
																}
															}
														}
													}
												}
											} else {
												let e = {};
												s === null ? s = [e] : s.push(e), c++;
											}
										}
										if (r !== c) break;
									}
								} else {
									let e = {};
									s === null ? s = [e] : s.push(e), c++;
								}
							}
							var m = n === c;
						} else var m = !0;
					}
				}
			}
		} else {
			let e = {};
			s === null ? s = [e] : s.push(e), c++;
		}
	}
	if (p === c) {
		let e = {};
		s === null ? s = [e] : s.push(e), c++;
	} else c = f, s !== null && (f ? s.length = f : s = null);
	return d === c ? (r.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (c = u, s !== null && (u ? s.length = u : s = null), r.errors = s, c === 0);
}
r.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
var i = a;
function a(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: i = e, dynamicAnchors: o = {} } = {}) {
	let s = null, c = 0, l = a.evaluated;
	l.dynamicProps && (l.props = void 0), l.dynamicItems && (l.items = void 0);
	let u = c, d = c, f = c, p = c;
	if (c === p) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.status === void 0 || e.enabled === void 0 || e.mode === void 0) {
				let e = {};
				s === null ? s = [e] : s.push(e), c++;
			} else {
				let t = c;
				for (let t in e) if (t !== "enabled" && t !== "mode" && t !== "status") {
					let e = {};
					s === null ? s = [e] : s.push(e), c++;
					break;
				}
				if (t === c) {
					if (e.enabled !== void 0) {
						let t = c;
						if (typeof e.enabled != "boolean") {
							let e = {};
							s === null ? s = [e] : s.push(e), c++;
						}
						var m = t === c;
					} else var m = !0;
					if (m) {
						if (e.mode !== void 0) {
							let t = e.mode, n = c;
							if (typeof t != "string") {
								let e = {};
								s === null ? s = [e] : s.push(e), c++;
							}
							if (t !== "whitelist" && t !== "blacklist") {
								let e = {};
								s === null ? s = [e] : s.push(e), c++;
							}
							var m = n === c;
						} else var m = !0;
						if (m) {
							if (e.status !== void 0) {
								let t = e.status, n = c;
								if (typeof t != "string") {
									let e = {};
									s === null ? s = [e] : s.push(e), c++;
								}
								if (t !== "ok") {
									let e = {};
									s === null ? s = [e] : s.push(e), c++;
								}
								var m = n === c;
							} else var m = !0;
						}
					}
				}
			}
		} else {
			let e = {};
			s === null ? s = [e] : s.push(e), c++;
		}
	}
	if (p === c) {
		let e = {};
		s === null ? s = [e] : s.push(e), c++;
	} else c = f, s !== null && (f ? s.length = f : s = null);
	return d === c ? (a.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (c = u, s !== null && (u ? s.length = u : s = null), a.errors = s, c === 0);
}
a.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
export { n, e as r, i as t };
