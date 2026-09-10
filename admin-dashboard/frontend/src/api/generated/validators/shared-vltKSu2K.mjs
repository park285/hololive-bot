var e = r, t = {
	$id: "urn:hololive:admin:assertion:5a4c9d2560a119e56620a5f7461286379257c085ecfdaf7bebd79bdf6be3e7cc",
	not: { not: {
		type: "object",
		required: ["status", "containers"],
		properties: {
			containers: {
				type: "array",
				items: {
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
				}
			},
			status: {
				type: "string",
				const: "ok"
			}
		},
		additionalProperties: !1
	} }
}, n = Object.prototype.hasOwnProperty;
function r(e, { instancePath: i = "", parentData: a, parentDataProperty: o, rootData: s = e, dynamicAnchors: c = {} } = {}) {
	let l = null, u = 0, d = r.evaluated;
	d.dynamicProps && (d.props = void 0), d.dynamicItems && (d.items = void 0);
	let f = u, p = u, m = u, h = u;
	if (u === h) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.status === void 0 || e.containers === void 0) {
				let e = {};
				l === null ? l = [e] : l.push(e), u++;
			} else {
				let r = u;
				for (let t in e) if (t !== "containers" && t !== "status") {
					let e = {};
					l === null ? l = [e] : l.push(e), u++;
					break;
				}
				if (r === u) {
					if (e.containers !== void 0) {
						let r = e.containers, i = u;
						if (u === i) {
							if (Array.isArray(r)) {
								let e = r.length;
								for (let i = 0; i < e; i++) {
									let e = r[i], a = u;
									if (u === a) {
										if (e && typeof e == "object" && !Array.isArray(e)) {
											if (e.id === void 0 || e.name === void 0 || e.image === void 0 || e.status === void 0 || e.state === void 0 || e.created === void 0 || e.ports === void 0 || e.managed === void 0 || e.stopBlocked === void 0) {
												let e = {};
												l === null ? l = [e] : l.push(e), u++;
											} else {
												let r = u;
												for (let r in e) if (!n.call(t.not.not.properties.containers.items.properties, r)) {
													let e = {};
													l === null ? l = [e] : l.push(e), u++;
													break;
												}
												if (r === u) {
													if (e.created !== void 0) {
														let t = e.created, n = u;
														if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
															let e = {};
															l === null ? l = [e] : l.push(e), u++;
														}
														var g = n === u;
													} else var g = !0;
													if (g) {
														if (e.health !== void 0) {
															let t = e.health, n = u;
															if (typeof t != "string" && t !== null) {
																let e = {};
																l === null ? l = [e] : l.push(e), u++;
															}
															var g = n === u;
														} else var g = !0;
														if (g) {
															if (e.id !== void 0) {
																let t = u;
																if (typeof e.id != "string") {
																	let e = {};
																	l === null ? l = [e] : l.push(e), u++;
																}
																var g = t === u;
															} else var g = !0;
															if (g) {
																if (e.image !== void 0) {
																	let t = u;
																	if (typeof e.image != "string") {
																		let e = {};
																		l === null ? l = [e] : l.push(e), u++;
																	}
																	var g = t === u;
																} else var g = !0;
																if (g) {
																	if (e.managed !== void 0) {
																		let t = u;
																		if (typeof e.managed != "boolean") {
																			let e = {};
																			l === null ? l = [e] : l.push(e), u++;
																		}
																		var g = t === u;
																	} else var g = !0;
																	if (g) {
																		if (e.name !== void 0) {
																			let t = u;
																			if (typeof e.name != "string") {
																				let e = {};
																				l === null ? l = [e] : l.push(e), u++;
																			}
																			var g = t === u;
																		} else var g = !0;
																		if (g) {
																			if (e.ports !== void 0) {
																				let t = e.ports, n = u;
																				if (u === n) {
																					if (Array.isArray(t)) {
																						let e = t.length;
																						for (let n = 0; n < e; n++) {
																							let e = t[n], r = u;
																							if (u === r) {
																								if (e && typeof e == "object" && !Array.isArray(e)) {
																									if (e.private_port === void 0 || e.port_type === void 0) {
																										let e = {};
																										l === null ? l = [e] : l.push(e), u++;
																									} else {
																										let t = u;
																										for (let t in e) if (t !== "port_type" && t !== "private_port" && t !== "public_port") {
																											let e = {};
																											l === null ? l = [e] : l.push(e), u++;
																											break;
																										}
																										if (t === u) {
																											if (e.port_type !== void 0) {
																												let t = u;
																												if (typeof e.port_type != "string") {
																													let e = {};
																													l === null ? l = [e] : l.push(e), u++;
																												}
																												var _ = t === u;
																											} else var _ = !0;
																											if (_) {
																												if (e.private_port !== void 0) {
																													let t = e.private_port, n = u;
																													if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																														let e = {};
																														l === null ? l = [e] : l.push(e), u++;
																													}
																													if (u === n && typeof t == "number" && isFinite(t)) {
																														if (t > 2147483647 || isNaN(t)) {
																															let e = {};
																															l === null ? l = [e] : l.push(e), u++;
																														} else if (t < 0 || isNaN(t)) {
																															let e = {};
																															l === null ? l = [e] : l.push(e), u++;
																														}
																													}
																													var _ = n === u;
																												} else var _ = !0;
																												if (_) {
																													if (e.public_port !== void 0) {
																														let t = e.public_port, n = u;
																														if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t)) && t !== null) {
																															let e = {};
																															l === null ? l = [e] : l.push(e), u++;
																														}
																														if (u === n && typeof t == "number" && isFinite(t)) {
																															if (t > 2147483647 || isNaN(t)) {
																																let e = {};
																																l === null ? l = [e] : l.push(e), u++;
																															} else if (t < 0 || isNaN(t)) {
																																let e = {};
																																l === null ? l = [e] : l.push(e), u++;
																															}
																														}
																														var _ = n === u;
																													} else var _ = !0;
																												}
																											}
																										}
																									}
																								} else {
																									let e = {};
																									l === null ? l = [e] : l.push(e), u++;
																								}
																							}
																							if (r !== u) break;
																						}
																					} else {
																						let e = {};
																						l === null ? l = [e] : l.push(e), u++;
																					}
																				}
																				var g = n === u;
																			} else var g = !0;
																			if (g) {
																				if (e.state !== void 0) {
																					let t = u;
																					if (typeof e.state != "string") {
																						let e = {};
																						l === null ? l = [e] : l.push(e), u++;
																					}
																					var g = t === u;
																				} else var g = !0;
																				if (g) {
																					if (e.status !== void 0) {
																						let t = u;
																						if (typeof e.status != "string") {
																							let e = {};
																							l === null ? l = [e] : l.push(e), u++;
																						}
																						var g = t === u;
																					} else var g = !0;
																					if (g) {
																						if (e.stopBlocked !== void 0) {
																							let t = u;
																							if (typeof e.stopBlocked != "boolean") {
																								let e = {};
																								l === null ? l = [e] : l.push(e), u++;
																							}
																							var g = t === u;
																						} else var g = !0;
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
											l === null ? l = [e] : l.push(e), u++;
										}
									}
									if (a !== u) break;
								}
							} else {
								let e = {};
								l === null ? l = [e] : l.push(e), u++;
							}
						}
						var v = i === u;
					} else var v = !0;
					if (v) {
						if (e.status !== void 0) {
							let t = e.status, n = u;
							if (typeof t != "string") {
								let e = {};
								l === null ? l = [e] : l.push(e), u++;
							}
							if (t !== "ok") {
								let e = {};
								l === null ? l = [e] : l.push(e), u++;
							}
							var v = n === u;
						} else var v = !0;
					}
				}
			}
		} else {
			let e = {};
			l === null ? l = [e] : l.push(e), u++;
		}
	}
	if (h === u) {
		let e = {};
		l === null ? l = [e] : l.push(e), u++;
	} else u = m, l !== null && (m ? l.length = m : l = null);
	return p === u ? (r.errors = [{
		instancePath: i,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (u = f, l !== null && (f ? l.length = f : l = null), r.errors = l, u === 0);
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
			if (e.status === void 0 || e.available === void 0) {
				let e = {};
				s === null ? s = [e] : s.push(e), c++;
			} else {
				let t = c;
				for (let t in e) if (t !== "available" && t !== "status") {
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
						var m = t === c;
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
export { e as n, i as t };
