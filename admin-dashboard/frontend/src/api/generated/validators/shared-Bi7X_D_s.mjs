var e = t;
function t(e, { instancePath: n = "", parentData: r, parentDataProperty: i, rootData: a = e, dynamicAnchors: o = {} } = {}) {
	let s = null, c = 0, l = t.evaluated;
	l.dynamicProps && (l.props = void 0), l.dynamicItems && (l.items = void 0);
	let u = c, d = c, f = c, p = c;
	if (c === p) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.status === void 0 || e.alarms === void 0) {
				let e = {};
				s === null ? s = [e] : s.push(e), c++;
			} else {
				let t = c;
				for (let t in e) if (t !== "alarms" && t !== "status") {
					let e = {};
					s === null ? s = [e] : s.push(e), c++;
					break;
				}
				if (t === c) {
					if (e.alarms !== void 0) {
						let t = e.alarms, n = c;
						if (c === n) {
							if (Array.isArray(t)) {
								let e = t.length;
								for (let n = 0; n < e; n++) {
									let e = t[n], r = c;
									if (c === r) {
										if (e && typeof e == "object" && !Array.isArray(e)) {
											if (e.roomId === void 0 || e.roomName === void 0 || e.channelId === void 0 || e.memberName === void 0) {
												let e = {};
												s === null ? s = [e] : s.push(e), c++;
											} else {
												let t = c;
												for (let t in e) if (t !== "channelId" && t !== "memberName" && t !== "roomId" && t !== "roomName") {
													let e = {};
													s === null ? s = [e] : s.push(e), c++;
													break;
												}
												if (t === c) {
													if (e.channelId !== void 0) {
														let t = c;
														if (typeof e.channelId != "string") {
															let e = {};
															s === null ? s = [e] : s.push(e), c++;
														}
														var m = t === c;
													} else var m = !0;
													if (m) {
														if (e.memberName !== void 0) {
															let t = c;
															if (typeof e.memberName != "string") {
																let e = {};
																s === null ? s = [e] : s.push(e), c++;
															}
															var m = t === c;
														} else var m = !0;
														if (m) {
															if (e.roomId !== void 0) {
																let t = c;
																if (typeof e.roomId != "string") {
																	let e = {};
																	s === null ? s = [e] : s.push(e), c++;
																}
																var m = t === c;
															} else var m = !0;
															if (m) {
																if (e.roomName !== void 0) {
																	let t = c;
																	if (typeof e.roomName != "string") {
																		let e = {};
																		s === null ? s = [e] : s.push(e), c++;
																	}
																	var m = t === c;
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
									if (r !== c) break;
								}
							} else {
								let e = {};
								s === null ? s = [e] : s.push(e), c++;
							}
						}
						var h = n === c;
					} else var h = !0;
					if (h) {
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
							var h = n === c;
						} else var h = !0;
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
			if (e.status === void 0 || e.removed === void 0) {
				let e = {};
				s === null ? s = [e] : s.push(e), c++;
			} else {
				let t = c;
				for (let t in e) if (t !== "status" && t !== "removed") {
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
						if (e.removed !== void 0) {
							let t = c;
							if (typeof e.removed != "boolean") {
								let e = {};
								s === null ? s = [e] : s.push(e), c++;
							}
							var m = t === c;
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
export { e as n, n as t };
