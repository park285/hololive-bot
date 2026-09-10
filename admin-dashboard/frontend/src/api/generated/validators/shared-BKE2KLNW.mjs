var e = r, t = {
	$id: "urn:hololive:admin:assertion:b9507895ac287bd35ee383547fd88d01515f5656e0d94b92ff30fc167b111c3c",
	not: { not: {
		type: "object",
		additionalProperties: !1,
		required: [
			"sampledAt",
			"cpuUsage",
			"memoryTotal",
			"memoryUsed",
			"memoryUsage",
			"threadCount",
			"totalGoGoroutines",
			"totalRuntimeUnits",
			"loadAvg1",
			"loadAvg5",
			"loadAvg15",
			"serviceRuntime"
		],
		properties: {
			sampledAt: {
				type: "integer",
				minimum: 0
			},
			cpuUsage: {
				type: "number",
				minimum: 0
			},
			memoryTotal: {
				type: "integer",
				minimum: 0
			},
			memoryUsed: {
				type: "integer",
				minimum: 0
			},
			memoryUsage: {
				type: "number",
				minimum: 0
			},
			threadCount: {
				type: "integer",
				minimum: 0
			},
			totalGoGoroutines: {
				type: "integer",
				minimum: 0
			},
			totalRuntimeUnits: {
				type: "integer",
				minimum: 0
			},
			loadAvg1: {
				type: "number",
				minimum: 0
			},
			loadAvg5: {
				type: "number",
				minimum: 0
			},
			loadAvg15: {
				type: "number",
				minimum: 0
			},
			serviceRuntime: {
				type: "array",
				items: {
					type: "object",
					additionalProperties: !1,
					required: [
						"name",
						"count",
						"metricKind",
						"available"
					],
					properties: {
						name: { type: "string" },
						count: {
							type: "integer",
							minimum: 0
						},
						metricKind: {
							type: "string",
							enum: ["goroutine", "thread"]
						},
						available: { type: "boolean" },
						error: { type: ["string", "null"] }
					}
				}
			}
		}
	} }
}, n = Object.prototype.hasOwnProperty;
function r(e, { instancePath: i = "", parentData: a, parentDataProperty: o, rootData: s = e, dynamicAnchors: c = {} } = {}) {
	let l = null, u = 0, d = r.evaluated;
	d.dynamicProps && (d.props = void 0), d.dynamicItems && (d.items = void 0);
	let f = u, p = u, m = u, h = u;
	if (u === h) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.sampledAt === void 0 || e.cpuUsage === void 0 || e.memoryTotal === void 0 || e.memoryUsed === void 0 || e.memoryUsage === void 0 || e.threadCount === void 0 || e.totalGoGoroutines === void 0 || e.totalRuntimeUnits === void 0 || e.loadAvg1 === void 0 || e.loadAvg5 === void 0 || e.loadAvg15 === void 0 || e.serviceRuntime === void 0) {
				let e = {};
				l === null ? l = [e] : l.push(e), u++;
			} else {
				let r = u;
				for (let r in e) if (!n.call(t.not.not.properties, r)) {
					let e = {};
					l === null ? l = [e] : l.push(e), u++;
					break;
				}
				if (r === u) {
					if (e.sampledAt !== void 0) {
						let t = e.sampledAt, n = u;
						if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
							let e = {};
							l === null ? l = [e] : l.push(e), u++;
						}
						if (u === n && typeof t == "number" && isFinite(t) && (t < 0 || isNaN(t))) {
							let e = {};
							l === null ? l = [e] : l.push(e), u++;
						}
						var g = n === u;
					} else var g = !0;
					if (g) {
						if (e.cpuUsage !== void 0) {
							let t = e.cpuUsage, n = u;
							if (u === n) {
								if (typeof t == "number" && isFinite(t)) {
									if (t < 0 || isNaN(t)) {
										let e = {};
										l === null ? l = [e] : l.push(e), u++;
									}
								} else {
									let e = {};
									l === null ? l = [e] : l.push(e), u++;
								}
							}
							var g = n === u;
						} else var g = !0;
						if (g) {
							if (e.memoryTotal !== void 0) {
								let t = e.memoryTotal, n = u;
								if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
									let e = {};
									l === null ? l = [e] : l.push(e), u++;
								}
								if (u === n && typeof t == "number" && isFinite(t) && (t < 0 || isNaN(t))) {
									let e = {};
									l === null ? l = [e] : l.push(e), u++;
								}
								var g = n === u;
							} else var g = !0;
							if (g) {
								if (e.memoryUsed !== void 0) {
									let t = e.memoryUsed, n = u;
									if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
										let e = {};
										l === null ? l = [e] : l.push(e), u++;
									}
									if (u === n && typeof t == "number" && isFinite(t) && (t < 0 || isNaN(t))) {
										let e = {};
										l === null ? l = [e] : l.push(e), u++;
									}
									var g = n === u;
								} else var g = !0;
								if (g) {
									if (e.memoryUsage !== void 0) {
										let t = e.memoryUsage, n = u;
										if (u === n) {
											if (typeof t == "number" && isFinite(t)) {
												if (t < 0 || isNaN(t)) {
													let e = {};
													l === null ? l = [e] : l.push(e), u++;
												}
											} else {
												let e = {};
												l === null ? l = [e] : l.push(e), u++;
											}
										}
										var g = n === u;
									} else var g = !0;
									if (g) {
										if (e.threadCount !== void 0) {
											let t = e.threadCount, n = u;
											if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
												let e = {};
												l === null ? l = [e] : l.push(e), u++;
											}
											if (u === n && typeof t == "number" && isFinite(t) && (t < 0 || isNaN(t))) {
												let e = {};
												l === null ? l = [e] : l.push(e), u++;
											}
											var g = n === u;
										} else var g = !0;
										if (g) {
											if (e.totalGoGoroutines !== void 0) {
												let t = e.totalGoGoroutines, n = u;
												if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
													let e = {};
													l === null ? l = [e] : l.push(e), u++;
												}
												if (u === n && typeof t == "number" && isFinite(t) && (t < 0 || isNaN(t))) {
													let e = {};
													l === null ? l = [e] : l.push(e), u++;
												}
												var g = n === u;
											} else var g = !0;
											if (g) {
												if (e.totalRuntimeUnits !== void 0) {
													let t = e.totalRuntimeUnits, n = u;
													if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
														let e = {};
														l === null ? l = [e] : l.push(e), u++;
													}
													if (u === n && typeof t == "number" && isFinite(t) && (t < 0 || isNaN(t))) {
														let e = {};
														l === null ? l = [e] : l.push(e), u++;
													}
													var g = n === u;
												} else var g = !0;
												if (g) {
													if (e.loadAvg1 !== void 0) {
														let t = e.loadAvg1, n = u;
														if (u === n) {
															if (typeof t == "number" && isFinite(t)) {
																if (t < 0 || isNaN(t)) {
																	let e = {};
																	l === null ? l = [e] : l.push(e), u++;
																}
															} else {
																let e = {};
																l === null ? l = [e] : l.push(e), u++;
															}
														}
														var g = n === u;
													} else var g = !0;
													if (g) {
														if (e.loadAvg5 !== void 0) {
															let t = e.loadAvg5, n = u;
															if (u === n) {
																if (typeof t == "number" && isFinite(t)) {
																	if (t < 0 || isNaN(t)) {
																		let e = {};
																		l === null ? l = [e] : l.push(e), u++;
																	}
																} else {
																	let e = {};
																	l === null ? l = [e] : l.push(e), u++;
																}
															}
															var g = n === u;
														} else var g = !0;
														if (g) {
															if (e.loadAvg15 !== void 0) {
																let t = e.loadAvg15, n = u;
																if (u === n) {
																	if (typeof t == "number" && isFinite(t)) {
																		if (t < 0 || isNaN(t)) {
																			let e = {};
																			l === null ? l = [e] : l.push(e), u++;
																		}
																	} else {
																		let e = {};
																		l === null ? l = [e] : l.push(e), u++;
																	}
																}
																var g = n === u;
															} else var g = !0;
															if (g) {
																if (e.serviceRuntime !== void 0) {
																	let t = e.serviceRuntime, n = u;
																	if (u === n) {
																		if (Array.isArray(t)) {
																			let e = t.length;
																			for (let n = 0; n < e; n++) {
																				let e = t[n], r = u;
																				if (u === r) {
																					if (e && typeof e == "object" && !Array.isArray(e)) {
																						if (e.name === void 0 || e.count === void 0 || e.metricKind === void 0 || e.available === void 0) {
																							let e = {};
																							l === null ? l = [e] : l.push(e), u++;
																						} else {
																							let t = u;
																							for (let t in e) if (t !== "name" && t !== "count" && t !== "metricKind" && t !== "available" && t !== "error") {
																								let e = {};
																								l === null ? l = [e] : l.push(e), u++;
																								break;
																							}
																							if (t === u) {
																								if (e.name !== void 0) {
																									let t = u;
																									if (typeof e.name != "string") {
																										let e = {};
																										l === null ? l = [e] : l.push(e), u++;
																									}
																									var _ = t === u;
																								} else var _ = !0;
																								if (_) {
																									if (e.count !== void 0) {
																										let t = e.count, n = u;
																										if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																											let e = {};
																											l === null ? l = [e] : l.push(e), u++;
																										}
																										if (u === n && typeof t == "number" && isFinite(t) && (t < 0 || isNaN(t))) {
																											let e = {};
																											l === null ? l = [e] : l.push(e), u++;
																										}
																										var _ = n === u;
																									} else var _ = !0;
																									if (_) {
																										if (e.metricKind !== void 0) {
																											let t = e.metricKind, n = u;
																											if (typeof t != "string") {
																												let e = {};
																												l === null ? l = [e] : l.push(e), u++;
																											}
																											if (t !== "goroutine" && t !== "thread") {
																												let e = {};
																												l === null ? l = [e] : l.push(e), u++;
																											}
																											var _ = n === u;
																										} else var _ = !0;
																										if (_) {
																											if (e.available !== void 0) {
																												let t = u;
																												if (typeof e.available != "boolean") {
																													let e = {};
																													l === null ? l = [e] : l.push(e), u++;
																												}
																												var _ = t === u;
																											} else var _ = !0;
																											if (_) {
																												if (e.error !== void 0) {
																													let t = e.error, n = u;
																													if (typeof t != "string" && t !== null) {
																														let e = {};
																														l === null ? l = [e] : l.push(e), u++;
																													}
																													var _ = n === u;
																												} else var _ = !0;
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
																				if (r !== u) break;
																			}
																		} else {
																			let e = {};
																			l === null ? l = [e] : l.push(e), u++;
																		}
																	}
																	var g = n === u;
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
