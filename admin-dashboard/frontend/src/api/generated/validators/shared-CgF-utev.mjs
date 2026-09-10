var e = t;
function t(e, { instancePath: n = "", parentData: r, parentDataProperty: i, rootData: a = e, dynamicAnchors: o = {} } = {}) {
	let s = null, c = 0, l = t.evaluated;
	l.dynamicProps && (l.props = void 0), l.dynamicItems && (l.items = void 0);
	let u = c, d = c, f = c, p = c;
	if (c === p) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.status === void 0 || e.members === void 0 || e.alarms === void 0 || e.rooms === void 0 || e.version === void 0 || e.uptime === void 0) {
				let e = {};
				s === null ? s = [e] : s.push(e), c++;
			} else {
				let t = c;
				for (let t in e) if (t !== "alarms" && t !== "members" && t !== "rooms" && t !== "status" && t !== "uptime" && t !== "version") {
					let e = {};
					s === null ? s = [e] : s.push(e), c++;
					break;
				}
				if (t === c) {
					if (e.alarms !== void 0) {
						let t = e.alarms, n = c;
						if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
							let e = {};
							s === null ? s = [e] : s.push(e), c++;
						}
						if (c === n && typeof t == "number" && isFinite(t)) {
							if (t > 2147483647 || isNaN(t)) {
								let e = {};
								s === null ? s = [e] : s.push(e), c++;
							} else if (t < -2147483648 || isNaN(t)) {
								let e = {};
								s === null ? s = [e] : s.push(e), c++;
							}
						}
						var m = n === c;
					} else var m = !0;
					if (m) {
						if (e.members !== void 0) {
							let t = e.members, n = c;
							if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
								let e = {};
								s === null ? s = [e] : s.push(e), c++;
							}
							if (c === n && typeof t == "number" && isFinite(t)) {
								if (t > 2147483647 || isNaN(t)) {
									let e = {};
									s === null ? s = [e] : s.push(e), c++;
								} else if (t < -2147483648 || isNaN(t)) {
									let e = {};
									s === null ? s = [e] : s.push(e), c++;
								}
							}
							var m = n === c;
						} else var m = !0;
						if (m) {
							if (e.rooms !== void 0) {
								let t = e.rooms, n = c;
								if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
									let e = {};
									s === null ? s = [e] : s.push(e), c++;
								}
								if (c === n && typeof t == "number" && isFinite(t)) {
									if (t > 2147483647 || isNaN(t)) {
										let e = {};
										s === null ? s = [e] : s.push(e), c++;
									} else if (t < -2147483648 || isNaN(t)) {
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
								if (m) {
									if (e.uptime !== void 0) {
										let t = c;
										if (typeof e.uptime != "string") {
											let e = {};
											s === null ? s = [e] : s.push(e), c++;
										}
										var m = t === c;
									} else var m = !0;
									if (m) {
										if (e.version !== void 0) {
											let t = c;
											if (typeof e.version != "string") {
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
			if (e.services === void 0 || e.uptime === void 0 || e.version === void 0 || e.sampled_at === void 0) {
				let e = {};
				s === null ? s = [e] : s.push(e), c++;
			} else {
				let t = c;
				for (let t in e) if (t !== "sampled_at" && t !== "services" && t !== "uptime" && t !== "version") {
					let e = {};
					s === null ? s = [e] : s.push(e), c++;
					break;
				}
				if (t === c) {
					if (e.sampled_at !== void 0) {
						let t = e.sampled_at, n = c;
						if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
							let e = {};
							s === null ? s = [e] : s.push(e), c++;
						}
						var m = n === c;
					} else var m = !0;
					if (m) {
						if (e.services !== void 0) {
							let t = e.services, n = c;
							if (c === n) {
								if (Array.isArray(t)) {
									let e = t.length;
									for (let n = 0; n < e; n++) {
										let e = t[n], r = c;
										if (c === r) {
											if (e && typeof e == "object" && !Array.isArray(e)) {
												if (e.name === void 0 || e.available === void 0) {
													let e = {};
													s === null ? s = [e] : s.push(e), c++;
												} else {
													let t = c;
													for (let t in e) if (t !== "available" && t !== "error" && t !== "name" && t !== "response_time_ms") {
														let e = {};
														s === null ? s = [e] : s.push(e), c++;
														break;
													}
													if (t === c) {
														if (e.available !== void 0) {
															let t = c;
															if (typeof e.available != "boolean") {
																let e = {};
																s === null ? s = [e] : s.push(e), c++;
															}
															var h = t === c;
														} else var h = !0;
														if (h) {
															if (e.error !== void 0) {
																let t = e.error, n = c;
																if (typeof t != "string" && t !== null) {
																	let e = {};
																	s === null ? s = [e] : s.push(e), c++;
																}
																var h = n === c;
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
																	if (e.response_time_ms !== void 0) {
																		let t = e.response_time_ms, n = c;
																		if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t)) && t !== null) {
																			let e = {};
																			s === null ? s = [e] : s.push(e), c++;
																		}
																		if (c === n && typeof t == "number" && isFinite(t) && (t < 0 || isNaN(t))) {
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
						if (m) {
							if (e.uptime !== void 0) {
								let t = c;
								if (typeof e.uptime != "string") {
									let e = {};
									s === null ? s = [e] : s.push(e), c++;
								}
								var m = t === c;
							} else var m = !0;
							if (m) {
								if (e.version !== void 0) {
									let t = c;
									if (typeof e.version != "string") {
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
