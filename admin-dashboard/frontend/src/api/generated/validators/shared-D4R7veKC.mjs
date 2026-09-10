import { n as e } from "./shared-t8Mukws9.mjs";
var t = r, n = e().default;
function r(e, { instancePath: t = "", parentData: i, parentDataProperty: a, rootData: o = e, dynamicAnchors: s = {} } = {}) {
	let c = null, l = 0, u = r.evaluated;
	u.dynamicProps && (u.props = void 0), u.dynamicItems && (u.items = void 0);
	let d = l, f = l, p = l, m = l;
	if (l === m) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.status === void 0 || e.authenticated === void 0 || e.username === void 0 || e.absolute_expires_at === void 0 || e.session_policy === void 0 || e.csrf_token === void 0) {
				let e = {};
				c === null ? c = [e] : c.push(e), l++;
			} else {
				let t = l;
				for (let t in e) if (t !== "absolute_expires_at" && t !== "authenticated" && t !== "csrf_token" && t !== "session_policy" && t !== "status" && t !== "username") {
					let e = {};
					c === null ? c = [e] : c.push(e), l++;
					break;
				}
				if (t === l) {
					if (e.absolute_expires_at !== void 0) {
						let t = e.absolute_expires_at, n = l;
						if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
							let e = {};
							c === null ? c = [e] : c.push(e), l++;
						}
						var h = n === l;
					} else var h = !0;
					if (h) {
						if (e.authenticated !== void 0) {
							let t = e.authenticated, n = l;
							if (typeof t != "boolean") {
								let e = {};
								c === null ? c = [e] : c.push(e), l++;
							}
							if (!0 !== t) {
								let e = {};
								c === null ? c = [e] : c.push(e), l++;
							}
							var h = n === l;
						} else var h = !0;
						if (h) {
							if (e.csrf_token !== void 0) {
								let t = e.csrf_token, r = l;
								if (l === r) {
									if (typeof t == "string") {
										if (n(t) < 1) {
											let e = {};
											c === null ? c = [e] : c.push(e), l++;
										}
									} else {
										let e = {};
										c === null ? c = [e] : c.push(e), l++;
									}
								}
								var h = r === l;
							} else var h = !0;
							if (h) {
								if (e.session_policy !== void 0) {
									let t = e.session_policy, n = l;
									if (l === n) {
										if (t && typeof t == "object" && !Array.isArray(t)) {
											if (t.heartbeat_interval_ms === void 0 || t.idle_timeout_ms === void 0 || t.idle_warning_timeout_ms === void 0 || t.idle_session_ttl_ms === void 0 || t.absolute_warning_window_ms === void 0) {
												let e = {};
												c === null ? c = [e] : c.push(e), l++;
											} else {
												let e = l;
												for (let e in t) if (e !== "absolute_warning_window_ms" && e !== "heartbeat_interval_ms" && e !== "idle_session_ttl_ms" && e !== "idle_timeout_ms" && e !== "idle_warning_timeout_ms") {
													let e = {};
													c === null ? c = [e] : c.push(e), l++;
													break;
												}
												if (e === l) {
													if (t.absolute_warning_window_ms !== void 0) {
														let e = t.absolute_warning_window_ms, n = l;
														if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
															let e = {};
															c === null ? c = [e] : c.push(e), l++;
														}
														if (l === n && typeof e == "number" && isFinite(e) && (e < 0 || isNaN(e))) {
															let e = {};
															c === null ? c = [e] : c.push(e), l++;
														}
														var g = n === l;
													} else var g = !0;
													if (g) {
														if (t.heartbeat_interval_ms !== void 0) {
															let e = t.heartbeat_interval_ms, n = l;
															if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
																let e = {};
																c === null ? c = [e] : c.push(e), l++;
															}
															if (l === n && typeof e == "number" && isFinite(e) && (e < 0 || isNaN(e))) {
																let e = {};
																c === null ? c = [e] : c.push(e), l++;
															}
															var g = n === l;
														} else var g = !0;
														if (g) {
															if (t.idle_session_ttl_ms !== void 0) {
																let e = t.idle_session_ttl_ms, n = l;
																if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
																	let e = {};
																	c === null ? c = [e] : c.push(e), l++;
																}
																if (l === n && typeof e == "number" && isFinite(e) && (e < 0 || isNaN(e))) {
																	let e = {};
																	c === null ? c = [e] : c.push(e), l++;
																}
																var g = n === l;
															} else var g = !0;
															if (g) {
																if (t.idle_timeout_ms !== void 0) {
																	let e = t.idle_timeout_ms, n = l;
																	if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
																		let e = {};
																		c === null ? c = [e] : c.push(e), l++;
																	}
																	if (l === n && typeof e == "number" && isFinite(e) && (e < 0 || isNaN(e))) {
																		let e = {};
																		c === null ? c = [e] : c.push(e), l++;
																	}
																	var g = n === l;
																} else var g = !0;
																if (g) {
																	if (t.idle_warning_timeout_ms !== void 0) {
																		let e = t.idle_warning_timeout_ms, n = l;
																		if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
																			let e = {};
																			c === null ? c = [e] : c.push(e), l++;
																		}
																		if (l === n && typeof e == "number" && isFinite(e) && (e < 0 || isNaN(e))) {
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
										} else {
											let e = {};
											c === null ? c = [e] : c.push(e), l++;
										}
									}
									var h = n === l;
								} else var h = !0;
								if (h) {
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
										var h = n === l;
									} else var h = !0;
									if (h) {
										if (e.username !== void 0) {
											let t = e.username, r = l;
											if (l === r) {
												if (typeof t == "string") {
													if (n(t) < 1) {
														let e = {};
														c === null ? c = [e] : c.push(e), l++;
													}
												} else {
													let e = {};
													c === null ? c = [e] : c.push(e), l++;
												}
											}
											var h = r === l;
										} else var h = !0;
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
	if (m === l) {
		let e = {};
		c === null ? c = [e] : c.push(e), l++;
	} else l = p, c !== null && (p ? c.length = p : c = null);
	return f === l ? (r.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (l = d, c !== null && (d ? c.length = d : c = null), r.errors = c, l === 0);
}
r.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
export { t };
