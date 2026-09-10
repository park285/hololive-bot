var e = t;
function t(e, { instancePath: n = "", parentData: r, parentDataProperty: i, rootData: a = e, dynamicAnchors: o = {} } = {}) {
	let s = null, c = 0, l = t.evaluated;
	l.dynamicProps && (l.props = void 0), l.dynamicItems && (l.items = void 0);
	let u = c, d = c, f = c, p = c;
	if (c === p) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.status === void 0 || e.settings === void 0) {
				let e = {};
				s === null ? s = [e] : s.push(e), c++;
			} else {
				let t = c;
				for (let t in e) if (t !== "settings" && t !== "status") {
					let e = {};
					s === null ? s = [e] : s.push(e), c++;
					break;
				}
				if (t === c) {
					if (e.settings !== void 0) {
						let t = e.settings, n = c;
						if (c === n) {
							if (t && typeof t == "object" && !Array.isArray(t)) {
								if (t.alarmAdvanceMinutes === void 0) {
									let e = {};
									s === null ? s = [e] : s.push(e), c++;
								} else {
									let e = c;
									for (let e in t) if (e !== "alarmAdvanceMinutes") {
										let e = {};
										s === null ? s = [e] : s.push(e), c++;
										break;
									}
									if (e === c && t.alarmAdvanceMinutes !== void 0) {
										let e = t.alarmAdvanceMinutes, n = c;
										if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
											let e = {};
											s === null ? s = [e] : s.push(e), c++;
										}
										if (c === n && typeof e == "number" && isFinite(e)) {
											if (e > 1440 || isNaN(e)) {
												let e = {};
												s === null ? s = [e] : s.push(e), c++;
											} else if (e < 0 || isNaN(e)) {
												let e = {};
												s === null ? s = [e] : s.push(e), c++;
											}
										}
									}
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
			if (e.status === void 0 || e.message === void 0 || e.settings === void 0 || e.runtime === void 0) {
				let e = {};
				s === null ? s = [e] : s.push(e), c++;
			} else {
				let t = c;
				for (let t in e) if (t !== "status" && t !== "message" && t !== "settings" && t !== "runtime") {
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
						if (e.message !== void 0) {
							let t = c;
							if (typeof e.message != "string") {
								let e = {};
								s === null ? s = [e] : s.push(e), c++;
							}
							var m = t === c;
						} else var m = !0;
						if (m) {
							if (e.settings !== void 0) {
								let t = e.settings, n = c;
								if (c === n) {
									if (t && typeof t == "object" && !Array.isArray(t)) {
										if (t.alarmAdvanceMinutes === void 0) {
											let e = {};
											s === null ? s = [e] : s.push(e), c++;
										} else {
											let e = c;
											for (let e in t) if (e !== "alarmAdvanceMinutes") {
												let e = {};
												s === null ? s = [e] : s.push(e), c++;
												break;
											}
											if (e === c && t.alarmAdvanceMinutes !== void 0) {
												let e = t.alarmAdvanceMinutes, n = c;
												if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
													let e = {};
													s === null ? s = [e] : s.push(e), c++;
												}
												if (c === n && typeof e == "number" && isFinite(e)) {
													if (e > 1440 || isNaN(e)) {
														let e = {};
														s === null ? s = [e] : s.push(e), c++;
													} else if (e < 0 || isNaN(e)) {
														let e = {};
														s === null ? s = [e] : s.push(e), c++;
													}
												}
											}
										}
									} else {
										let e = {};
										s === null ? s = [e] : s.push(e), c++;
									}
								}
								var m = n === c;
							} else var m = !0;
							if (m) {
								if (e.runtime !== void 0) {
									let t = e.runtime, n = c;
									if (c === n) {
										if (t && typeof t == "object" && !Array.isArray(t)) {
											if (t.alarm_applied !== void 0) {
												let e = c;
												if (typeof t.alarm_applied != "boolean") {
													let e = {};
													s === null ? s = [e] : s.push(e), c++;
												}
												var h = e === c;
											} else var h = !0;
											if (h) {
												if (t.alarm_requested_advance_minutes !== void 0) {
													let e = t.alarm_requested_advance_minutes, n = c;
													if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
														let e = {};
														s === null ? s = [e] : s.push(e), c++;
													}
													var h = n === c;
												} else var h = !0;
												if (h) {
													if (t.alarm_reason !== void 0) {
														let e = c;
														if (typeof t.alarm_reason != "string") {
															let e = {};
															s === null ? s = [e] : s.push(e), c++;
														}
														var h = e === c;
													} else var h = !0;
													if (h) {
														if (t.alarm_target_minutes !== void 0) {
															let e = t.alarm_target_minutes, n = c;
															if (c === n) {
																if (Array.isArray(e)) {
																	let t = e.length;
																	for (let n = 0; n < t; n++) {
																		let t = e[n], r = c;
																		if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																			let e = {};
																			s === null ? s = [e] : s.push(e), c++;
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
															if (t.config_publish_alarm_advance_minutes !== void 0) {
																let e = c;
																if (typeof t.config_publish_alarm_advance_minutes != "boolean") {
																	let e = {};
																	s === null ? s = [e] : s.push(e), c++;
																}
																var h = e === c;
															} else var h = !0;
															if (h) {
																if (t.config_publish_alarm_advance_minutes_error !== void 0) {
																	let e = c;
																	if (typeof t.config_publish_alarm_advance_minutes_error != "string") {
																		let e = {};
																		s === null ? s = [e] : s.push(e), c++;
																	}
																	var h = e === c;
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
