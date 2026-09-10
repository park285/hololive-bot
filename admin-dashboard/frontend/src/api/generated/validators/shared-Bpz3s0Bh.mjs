var e = r, t = {
	$id: "urn:hololive:admin:assertion:c9c93dcc72d222fa2341720928f10a62f5b203e6d15e1613e5a11675129762bb",
	not: { not: {
		type: "object",
		required: [
			"status",
			"generatedAt",
			"windowStart",
			"windowEnd",
			"windowHours",
			"observedAtBasis",
			"slaThresholdMillis",
			"overview",
			"channels"
		],
		properties: {
			channels: {
				type: "array",
				items: {
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
				}
			},
			generatedAt: { type: "string" },
			observedAtBasis: { type: "string" },
			overview: {
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
			},
			slaThresholdMillis: { type: "integer" },
			status: {
				type: "string",
				const: "ok"
			},
			windowEnd: { type: "string" },
			windowHours: { type: "integer" },
			windowStart: { type: "string" }
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
			if (e.status === void 0 || e.generatedAt === void 0 || e.windowStart === void 0 || e.windowEnd === void 0 || e.windowHours === void 0 || e.observedAtBasis === void 0 || e.slaThresholdMillis === void 0 || e.overview === void 0 || e.channels === void 0) {
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
					if (e.channels !== void 0) {
						let r = e.channels, i = u;
						if (u === i) {
							if (Array.isArray(r)) {
								let e = r.length;
								for (let i = 0; i < e; i++) {
									let e = r[i], a = u;
									if (u === a) {
										if (e && typeof e == "object" && !Array.isArray(e)) {
											if (e.channelId === void 0 || e.detectedPostCount === void 0 || e.alarmSentPostCount === void 0 || e.successPostCount === void 0 || e.failedPostCount === void 0 || e.detectedUnsentPostCount === void 0 || e.pendingPostCount === void 0 || e.latencyMeasuredPostCount === void 0 || e.withinTargetPostCount === void 0 || e.exceededPostCount === void 0 || e.communityPostCount === void 0 || e.shortsPostCount === void 0) {
												let e = {};
												l === null ? l = [e] : l.push(e), u++;
											} else {
												let r = u;
												for (let r in e) if (!n.call(t.not.not.properties.channels.items.properties, r)) {
													let e = {};
													l === null ? l = [e] : l.push(e), u++;
													break;
												}
												if (r === u) {
													if (e.alarmSentPostCount !== void 0) {
														let t = e.alarmSentPostCount, n = u;
														if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
															let e = {};
															l === null ? l = [e] : l.push(e), u++;
														}
														var g = n === u;
													} else var g = !0;
													if (g) {
														if (e.averageLatencyMillis !== void 0) {
															let t = e.averageLatencyMillis, n = u;
															if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t)) && t !== null) {
																let e = {};
																l === null ? l = [e] : l.push(e), u++;
															}
															var g = n === u;
														} else var g = !0;
														if (g) {
															if (e.channelId !== void 0) {
																let t = u;
																if (typeof e.channelId != "string") {
																	let e = {};
																	l === null ? l = [e] : l.push(e), u++;
																}
																var g = t === u;
															} else var g = !0;
															if (g) {
																if (e.communityPostCount !== void 0) {
																	let t = e.communityPostCount, n = u;
																	if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																		let e = {};
																		l === null ? l = [e] : l.push(e), u++;
																	}
																	var g = n === u;
																} else var g = !0;
																if (g) {
																	if (e.detectedPostCount !== void 0) {
																		let t = e.detectedPostCount, n = u;
																		if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																			let e = {};
																			l === null ? l = [e] : l.push(e), u++;
																		}
																		var g = n === u;
																	} else var g = !0;
																	if (g) {
																		if (e.detectedUnsentPostCount !== void 0) {
																			let t = e.detectedUnsentPostCount, n = u;
																			if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																				let e = {};
																				l === null ? l = [e] : l.push(e), u++;
																			}
																			var g = n === u;
																		} else var g = !0;
																		if (g) {
																			if (e.earliestObservedAt !== void 0) {
																				let t = e.earliestObservedAt, n = u;
																				if (typeof t != "string" && t !== null) {
																					let e = {};
																					l === null ? l = [e] : l.push(e), u++;
																				}
																				var g = n === u;
																			} else var g = !0;
																			if (g) {
																				if (e.exceededPostCount !== void 0) {
																					let t = e.exceededPostCount, n = u;
																					if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																						let e = {};
																						l === null ? l = [e] : l.push(e), u++;
																					}
																					var g = n === u;
																				} else var g = !0;
																				if (g) {
																					if (e.failedPostCount !== void 0) {
																						let t = e.failedPostCount, n = u;
																						if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																							let e = {};
																							l === null ? l = [e] : l.push(e), u++;
																						}
																						var g = n === u;
																					} else var g = !0;
																					if (g) {
																						if (e.latencyMeasuredPostCount !== void 0) {
																							let t = e.latencyMeasuredPostCount, n = u;
																							if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																								let e = {};
																								l === null ? l = [e] : l.push(e), u++;
																							}
																							var g = n === u;
																						} else var g = !0;
																						if (g) {
																							if (e.latestObservedAt !== void 0) {
																								let t = e.latestObservedAt, n = u;
																								if (typeof t != "string" && t !== null) {
																									let e = {};
																									l === null ? l = [e] : l.push(e), u++;
																								}
																								var g = n === u;
																							} else var g = !0;
																							if (g) {
																								if (e.maxLatencyMillis !== void 0) {
																									let t = e.maxLatencyMillis, n = u;
																									if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t)) && t !== null) {
																										let e = {};
																										l === null ? l = [e] : l.push(e), u++;
																									}
																									var g = n === u;
																								} else var g = !0;
																								if (g) {
																									if (e.memberName !== void 0) {
																										let t = e.memberName, n = u;
																										if (typeof t != "string" && t !== null) {
																											let e = {};
																											l === null ? l = [e] : l.push(e), u++;
																										}
																										var g = n === u;
																									} else var g = !0;
																									if (g) {
																										if (e.pendingPostCount !== void 0) {
																											let t = e.pendingPostCount, n = u;
																											if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																												let e = {};
																												l === null ? l = [e] : l.push(e), u++;
																											}
																											var g = n === u;
																										} else var g = !0;
																										if (g) {
																											if (e.shortsPostCount !== void 0) {
																												let t = e.shortsPostCount, n = u;
																												if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																													let e = {};
																													l === null ? l = [e] : l.push(e), u++;
																												}
																												var g = n === u;
																											} else var g = !0;
																											if (g) {
																												if (e.successPostCount !== void 0) {
																													let t = e.successPostCount, n = u;
																													if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																														let e = {};
																														l === null ? l = [e] : l.push(e), u++;
																													}
																													var g = n === u;
																												} else var g = !0;
																												if (g) {
																													if (e.withinTargetPostCount !== void 0) {
																														let t = e.withinTargetPostCount, n = u;
																														if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
																															let e = {};
																															l === null ? l = [e] : l.push(e), u++;
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
						var _ = i === u;
					} else var _ = !0;
					if (_) {
						if (e.generatedAt !== void 0) {
							let t = u;
							if (typeof e.generatedAt != "string") {
								let e = {};
								l === null ? l = [e] : l.push(e), u++;
							}
							var _ = t === u;
						} else var _ = !0;
						if (_) {
							if (e.observedAtBasis !== void 0) {
								let t = u;
								if (typeof e.observedAtBasis != "string") {
									let e = {};
									l === null ? l = [e] : l.push(e), u++;
								}
								var _ = t === u;
							} else var _ = !0;
							if (_) {
								if (e.overview !== void 0) {
									let r = e.overview, i = u;
									if (u === i) {
										if (r && typeof r == "object" && !Array.isArray(r)) {
											if (r.channelCount === void 0 || r.detectedPostCount === void 0 || r.alarmSentPostCount === void 0 || r.successPostCount === void 0 || r.failedPostCount === void 0 || r.detectedUnsentPostCount === void 0 || r.pendingPostCount === void 0 || r.latencyMeasuredPostCount === void 0 || r.withinTargetPostCount === void 0 || r.exceededPostCount === void 0 || r.communityDetectedPostCount === void 0 || r.shortsDetectedPostCount === void 0 || r.communityExceededPostCount === void 0 || r.shortsExceededPostCount === void 0) {
												let e = {};
												l === null ? l = [e] : l.push(e), u++;
											} else {
												let e = u;
												for (let e in r) if (!n.call(t.not.not.properties.overview.properties, e)) {
													let e = {};
													l === null ? l = [e] : l.push(e), u++;
													break;
												}
												if (e === u) {
													if (r.alarmSentPostCount !== void 0) {
														let e = r.alarmSentPostCount, t = u;
														if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
															let e = {};
															l === null ? l = [e] : l.push(e), u++;
														}
														var v = t === u;
													} else var v = !0;
													if (v) {
														if (r.averageLatencyMillis !== void 0) {
															let e = r.averageLatencyMillis, t = u;
															if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e)) && e !== null) {
																let e = {};
																l === null ? l = [e] : l.push(e), u++;
															}
															var v = t === u;
														} else var v = !0;
														if (v) {
															if (r.channelCount !== void 0) {
																let e = r.channelCount, t = u;
																if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
																	let e = {};
																	l === null ? l = [e] : l.push(e), u++;
																}
																var v = t === u;
															} else var v = !0;
															if (v) {
																if (r.communityDetectedPostCount !== void 0) {
																	let e = r.communityDetectedPostCount, t = u;
																	if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
																		let e = {};
																		l === null ? l = [e] : l.push(e), u++;
																	}
																	var v = t === u;
																} else var v = !0;
																if (v) {
																	if (r.communityExceededPostCount !== void 0) {
																		let e = r.communityExceededPostCount, t = u;
																		if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
																			let e = {};
																			l === null ? l = [e] : l.push(e), u++;
																		}
																		var v = t === u;
																	} else var v = !0;
																	if (v) {
																		if (r.detectedPostCount !== void 0) {
																			let e = r.detectedPostCount, t = u;
																			if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
																				let e = {};
																				l === null ? l = [e] : l.push(e), u++;
																			}
																			var v = t === u;
																		} else var v = !0;
																		if (v) {
																			if (r.detectedUnsentPostCount !== void 0) {
																				let e = r.detectedUnsentPostCount, t = u;
																				if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
																					let e = {};
																					l === null ? l = [e] : l.push(e), u++;
																				}
																				var v = t === u;
																			} else var v = !0;
																			if (v) {
																				if (r.exceededPostCount !== void 0) {
																					let e = r.exceededPostCount, t = u;
																					if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
																						let e = {};
																						l === null ? l = [e] : l.push(e), u++;
																					}
																					var v = t === u;
																				} else var v = !0;
																				if (v) {
																					if (r.failedPostCount !== void 0) {
																						let e = r.failedPostCount, t = u;
																						if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
																							let e = {};
																							l === null ? l = [e] : l.push(e), u++;
																						}
																						var v = t === u;
																					} else var v = !0;
																					if (v) {
																						if (r.latencyMeasuredPostCount !== void 0) {
																							let e = r.latencyMeasuredPostCount, t = u;
																							if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
																								let e = {};
																								l === null ? l = [e] : l.push(e), u++;
																							}
																							var v = t === u;
																						} else var v = !0;
																						if (v) {
																							if (r.maxLatencyMillis !== void 0) {
																								let e = r.maxLatencyMillis, t = u;
																								if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e)) && e !== null) {
																									let e = {};
																									l === null ? l = [e] : l.push(e), u++;
																								}
																								var v = t === u;
																							} else var v = !0;
																							if (v) {
																								if (r.pendingPostCount !== void 0) {
																									let e = r.pendingPostCount, t = u;
																									if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
																										let e = {};
																										l === null ? l = [e] : l.push(e), u++;
																									}
																									var v = t === u;
																								} else var v = !0;
																								if (v) {
																									if (r.shortsDetectedPostCount !== void 0) {
																										let e = r.shortsDetectedPostCount, t = u;
																										if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
																											let e = {};
																											l === null ? l = [e] : l.push(e), u++;
																										}
																										var v = t === u;
																									} else var v = !0;
																									if (v) {
																										if (r.shortsExceededPostCount !== void 0) {
																											let e = r.shortsExceededPostCount, t = u;
																											if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
																												let e = {};
																												l === null ? l = [e] : l.push(e), u++;
																											}
																											var v = t === u;
																										} else var v = !0;
																										if (v) {
																											if (r.successPostCount !== void 0) {
																												let e = r.successPostCount, t = u;
																												if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
																													let e = {};
																													l === null ? l = [e] : l.push(e), u++;
																												}
																												var v = t === u;
																											} else var v = !0;
																											if (v) {
																												if (r.withinTargetPostCount !== void 0) {
																													let e = r.withinTargetPostCount, t = u;
																													if (!(typeof e == "number" && !(e % 1) && !isNaN(e) && isFinite(e))) {
																														let e = {};
																														l === null ? l = [e] : l.push(e), u++;
																													}
																													var v = t === u;
																												} else var v = !0;
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
											l === null ? l = [e] : l.push(e), u++;
										}
									}
									var _ = i === u;
								} else var _ = !0;
								if (_) {
									if (e.slaThresholdMillis !== void 0) {
										let t = e.slaThresholdMillis, n = u;
										if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
											let e = {};
											l === null ? l = [e] : l.push(e), u++;
										}
										var _ = n === u;
									} else var _ = !0;
									if (_) {
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
											var _ = n === u;
										} else var _ = !0;
										if (_) {
											if (e.windowEnd !== void 0) {
												let t = u;
												if (typeof e.windowEnd != "string") {
													let e = {};
													l === null ? l = [e] : l.push(e), u++;
												}
												var _ = t === u;
											} else var _ = !0;
											if (_) {
												if (e.windowHours !== void 0) {
													let t = e.windowHours, n = u;
													if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
														let e = {};
														l === null ? l = [e] : l.push(e), u++;
													}
													var _ = n === u;
												} else var _ = !0;
												if (_) {
													if (e.windowStart !== void 0) {
														let t = u;
														if (typeof e.windowStart != "string") {
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
			if (e.openapi === void 0 || e.info === void 0 || e.paths === void 0 || e.components === void 0) {
				let e = {};
				s === null ? s = [e] : s.push(e), c++;
			} else {
				if (e.openapi !== void 0) {
					let t = c;
					if (e.openapi !== "3.1.0") {
						let e = {};
						s === null ? s = [e] : s.push(e), c++;
					}
					var m = t === c;
				} else var m = !0;
				if (m) {
					if (e.info !== void 0) {
						let t = e.info, n = c;
						if (c === n) {
							if (t && typeof t == "object" && !Array.isArray(t)) {
								if (t.title === void 0 || t.version === void 0) {
									let e = {};
									s === null ? s = [e] : s.push(e), c++;
								} else {
									if (t.title !== void 0) {
										let e = c;
										if (typeof t.title != "string") {
											let e = {};
											s === null ? s = [e] : s.push(e), c++;
										}
										var h = e === c;
									} else var h = !0;
									if (h) {
										if (t.version !== void 0) {
											let e = c;
											if (typeof t.version != "string") {
												let e = {};
												s === null ? s = [e] : s.push(e), c++;
											}
											var h = e === c;
										} else var h = !0;
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
						if (e.paths !== void 0) {
							let t = e.paths, n = c;
							if (c === n) {
								if (t && typeof t == "object" && !Array.isArray(t)) for (let e in t) {
									let n = t[e], r = c;
									if (!(n && typeof n == "object" && !Array.isArray(n))) {
										let e = {};
										s === null ? s = [e] : s.push(e), c++;
									}
									if (r !== c) break;
								}
								else {
									let e = {};
									s === null ? s = [e] : s.push(e), c++;
								}
							}
							var m = n === c;
						} else var m = !0;
						if (m) {
							if (e.components !== void 0) {
								let t = e.components, n = c;
								if (c === n && !(t && typeof t == "object" && !Array.isArray(t))) {
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
export { e as n, i as t };
