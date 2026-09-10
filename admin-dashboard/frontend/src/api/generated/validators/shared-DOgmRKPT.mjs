var e = r, t = {
	$id: "urn:hololive:admin:assertion:af31175d16d7526698fe6641707570ee524c138e97bd7d4b6ec41f5ae5e4d579",
	not: { not: {
		type: "object",
		required: ["status", "streams"],
		properties: {
			org: { type: ["string", "null"] },
			status: {
				type: "string",
				const: "ok"
			},
			streams: {
				type: "array",
				items: {
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
				}
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
			if (e.status === void 0 || e.streams === void 0) {
				let e = {};
				l === null ? l = [e] : l.push(e), u++;
			} else {
				let r = u;
				for (let t in e) if (t !== "org" && t !== "status" && t !== "streams") {
					let e = {};
					l === null ? l = [e] : l.push(e), u++;
					break;
				}
				if (r === u) {
					if (e.org !== void 0) {
						let t = e.org, n = u;
						if (typeof t != "string" && t !== null) {
							let e = {};
							l === null ? l = [e] : l.push(e), u++;
						}
						var g = n === u;
					} else var g = !0;
					if (g) {
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
							var g = n === u;
						} else var g = !0;
						if (g) {
							if (e.streams !== void 0) {
								let r = e.streams, i = u;
								if (u === i) {
									if (Array.isArray(r)) {
										let e = r.length;
										for (let i = 0; i < e; i++) {
											let e = r[i], a = u;
											if (u === a) {
												if (e && typeof e == "object" && !Array.isArray(e)) {
													if (e.id === void 0 || e.title === void 0 || e.status === void 0 || e.channel_id === void 0) {
														let e = {};
														l === null ? l = [e] : l.push(e), u++;
													} else {
														let r = u;
														for (let r in e) if (!n.call(t.not.not.properties.streams.items.properties, r)) {
															let e = {};
															l === null ? l = [e] : l.push(e), u++;
															break;
														}
														if (r === u) {
															if (e.channel_id !== void 0) {
																let t = u;
																if (typeof e.channel_id != "string") {
																	let e = {};
																	l === null ? l = [e] : l.push(e), u++;
																}
																var _ = t === u;
															} else var _ = !0;
															if (_) {
																if (e.channel_name !== void 0) {
																	let t = e.channel_name, n = u;
																	if (typeof t != "string" && t !== null) {
																		let e = {};
																		l === null ? l = [e] : l.push(e), u++;
																	}
																	var _ = n === u;
																} else var _ = !0;
																if (_) {
																	if (e.id !== void 0) {
																		let t = u;
																		if (typeof e.id != "string") {
																			let e = {};
																			l === null ? l = [e] : l.push(e), u++;
																		}
																		var _ = t === u;
																	} else var _ = !0;
																	if (_) {
																		if (e.link !== void 0) {
																			let t = e.link, n = u;
																			if (typeof t != "string" && t !== null) {
																				let e = {};
																				l === null ? l = [e] : l.push(e), u++;
																			}
																			var _ = n === u;
																		} else var _ = !0;
																		if (_) {
																			if (e.start_actual !== void 0) {
																				let t = e.start_actual, n = u;
																				if (typeof t != "string" && t !== null) {
																					let e = {};
																					l === null ? l = [e] : l.push(e), u++;
																				}
																				var _ = n === u;
																			} else var _ = !0;
																			if (_) {
																				if (e.start_scheduled !== void 0) {
																					let t = e.start_scheduled, n = u;
																					if (typeof t != "string" && t !== null) {
																						let e = {};
																						l === null ? l = [e] : l.push(e), u++;
																					}
																					var _ = n === u;
																				} else var _ = !0;
																				if (_) {
																					if (e.status !== void 0) {
																						let t = u;
																						if (typeof e.status != "string") {
																							let e = {};
																							l === null ? l = [e] : l.push(e), u++;
																						}
																						var _ = t === u;
																					} else var _ = !0;
																					if (_) {
																						if (e.thumbnail !== void 0) {
																							let t = e.thumbnail, n = u;
																							if (typeof t != "string" && t !== null) {
																								let e = {};
																								l === null ? l = [e] : l.push(e), u++;
																							}
																							var _ = n === u;
																						} else var _ = !0;
																						if (_) {
																							if (e.title !== void 0) {
																								let t = u;
																								if (typeof e.title != "string") {
																									let e = {};
																									l === null ? l = [e] : l.push(e), u++;
																								}
																								var _ = t === u;
																							} else var _ = !0;
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
								var g = i === u;
							} else var g = !0;
						}
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
export { e as t };
