var e = i, t = {
	$id: "urn:hololive:admin:assertion:adc38a36caa06cac730279df47a4ec9fb5fa9a443bff224091e7176648a54c4c",
	not: { not: {
		type: "object",
		required: [
			"status",
			"month",
			"year",
			"entries"
		],
		properties: {
			entries: {
				type: "array",
				items: {
					type: "object",
					required: [
						"kind",
						"member",
						"day"
					],
					properties: {
						day: {
							type: "integer",
							minimum: -2147483648,
							maximum: 2147483647
						},
						kind: { type: "string" },
						member: {
							type: "object",
							required: [
								"id",
								"channelId",
								"name"
							],
							properties: {
								channelId: { type: "string" },
								id: {
									type: "string",
									pattern: "^[1-9][0-9]*$"
								},
								isGraduated: { type: "boolean" },
								name: { type: "string" },
								nameKo: { type: ["string", "null"] },
								org: { type: ["string", "null"] },
								photo: { type: ["string", "null"] },
								shortKoreanName: { type: ["string", "null"] },
								suborg: { type: ["string", "null"] }
							},
							additionalProperties: !1
						},
						ordinal: {
							type: ["integer", "null"],
							minimum: -2147483648,
							maximum: 2147483647
						}
					},
					additionalProperties: !1
				}
			},
			month: {
				type: "integer",
				minimum: -2147483648,
				maximum: 2147483647
			},
			status: {
				type: "string",
				const: "ok"
			},
			year: {
				type: "integer",
				minimum: -2147483648,
				maximum: 2147483647
			}
		},
		additionalProperties: !1
	} }
}, n = Object.prototype.hasOwnProperty, r = /* @__PURE__ */ RegExp("^[1-9][0-9]*$", "u");
function i(e, { instancePath: a = "", parentData: o, parentDataProperty: s, rootData: c = e, dynamicAnchors: l = {} } = {}) {
	let u = null, d = 0, f = i.evaluated;
	f.dynamicProps && (f.props = void 0), f.dynamicItems && (f.items = void 0);
	let p = d, m = d, h = d, g = d;
	if (d === g) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.status === void 0 || e.month === void 0 || e.year === void 0 || e.entries === void 0) {
				let e = {};
				u === null ? u = [e] : u.push(e), d++;
			} else {
				let i = d;
				for (let t in e) if (t !== "entries" && t !== "month" && t !== "status" && t !== "year") {
					let e = {};
					u === null ? u = [e] : u.push(e), d++;
					break;
				}
				if (i === d) {
					if (e.entries !== void 0) {
						let i = e.entries, a = d;
						if (d === a) {
							if (Array.isArray(i)) {
								let e = i.length;
								for (let a = 0; a < e; a++) {
									let e = i[a], o = d;
									if (d === o) {
										if (e && typeof e == "object" && !Array.isArray(e)) {
											if (e.kind === void 0 || e.member === void 0 || e.day === void 0) {
												let e = {};
												u === null ? u = [e] : u.push(e), d++;
											} else {
												let i = d;
												for (let t in e) if (t !== "day" && t !== "kind" && t !== "member" && t !== "ordinal") {
													let e = {};
													u === null ? u = [e] : u.push(e), d++;
													break;
												}
												if (i === d) {
													if (e.day !== void 0) {
														let t = e.day, n = d;
														if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
															let e = {};
															u === null ? u = [e] : u.push(e), d++;
														}
														if (d === n && typeof t == "number" && isFinite(t)) {
															if (t > 2147483647 || isNaN(t)) {
																let e = {};
																u === null ? u = [e] : u.push(e), d++;
															} else if (t < -2147483648 || isNaN(t)) {
																let e = {};
																u === null ? u = [e] : u.push(e), d++;
															}
														}
														var _ = n === d;
													} else var _ = !0;
													if (_) {
														if (e.kind !== void 0) {
															let t = d;
															if (typeof e.kind != "string") {
																let e = {};
																u === null ? u = [e] : u.push(e), d++;
															}
															var _ = t === d;
														} else var _ = !0;
														if (_) {
															if (e.member !== void 0) {
																let i = e.member, a = d;
																if (d === a) {
																	if (i && typeof i == "object" && !Array.isArray(i)) {
																		if (i.id === void 0 || i.channelId === void 0 || i.name === void 0) {
																			let e = {};
																			u === null ? u = [e] : u.push(e), d++;
																		} else {
																			let e = d;
																			for (let e in i) if (!n.call(t.not.not.properties.entries.items.properties.member.properties, e)) {
																				let e = {};
																				u === null ? u = [e] : u.push(e), d++;
																				break;
																			}
																			if (e === d) {
																				if (i.channelId !== void 0) {
																					let e = d;
																					if (typeof i.channelId != "string") {
																						let e = {};
																						u === null ? u = [e] : u.push(e), d++;
																					}
																					var v = e === d;
																				} else var v = !0;
																				if (v) {
																					if (i.id !== void 0) {
																						let e = i.id, t = d;
																						if (d === t) {
																							if (typeof e == "string") {
																								if (!r.test(e)) {
																									let e = {};
																									u === null ? u = [e] : u.push(e), d++;
																								}
																							} else {
																								let e = {};
																								u === null ? u = [e] : u.push(e), d++;
																							}
																						}
																						var v = t === d;
																					} else var v = !0;
																					if (v) {
																						if (i.isGraduated !== void 0) {
																							let e = d;
																							if (typeof i.isGraduated != "boolean") {
																								let e = {};
																								u === null ? u = [e] : u.push(e), d++;
																							}
																							var v = e === d;
																						} else var v = !0;
																						if (v) {
																							if (i.name !== void 0) {
																								let e = d;
																								if (typeof i.name != "string") {
																									let e = {};
																									u === null ? u = [e] : u.push(e), d++;
																								}
																								var v = e === d;
																							} else var v = !0;
																							if (v) {
																								if (i.nameKo !== void 0) {
																									let e = i.nameKo, t = d;
																									if (typeof e != "string" && e !== null) {
																										let e = {};
																										u === null ? u = [e] : u.push(e), d++;
																									}
																									var v = t === d;
																								} else var v = !0;
																								if (v) {
																									if (i.org !== void 0) {
																										let e = i.org, t = d;
																										if (typeof e != "string" && e !== null) {
																											let e = {};
																											u === null ? u = [e] : u.push(e), d++;
																										}
																										var v = t === d;
																									} else var v = !0;
																									if (v) {
																										if (i.photo !== void 0) {
																											let e = i.photo, t = d;
																											if (typeof e != "string" && e !== null) {
																												let e = {};
																												u === null ? u = [e] : u.push(e), d++;
																											}
																											var v = t === d;
																										} else var v = !0;
																										if (v) {
																											if (i.shortKoreanName !== void 0) {
																												let e = i.shortKoreanName, t = d;
																												if (typeof e != "string" && e !== null) {
																													let e = {};
																													u === null ? u = [e] : u.push(e), d++;
																												}
																												var v = t === d;
																											} else var v = !0;
																											if (v) {
																												if (i.suborg !== void 0) {
																													let e = i.suborg, t = d;
																													if (typeof e != "string" && e !== null) {
																														let e = {};
																														u === null ? u = [e] : u.push(e), d++;
																													}
																													var v = t === d;
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
																	} else {
																		let e = {};
																		u === null ? u = [e] : u.push(e), d++;
																	}
																}
																var _ = a === d;
															} else var _ = !0;
															if (_) {
																if (e.ordinal !== void 0) {
																	let t = e.ordinal, n = d;
																	if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t)) && t !== null) {
																		let e = {};
																		u === null ? u = [e] : u.push(e), d++;
																	}
																	if (d === n && typeof t == "number" && isFinite(t)) {
																		if (t > 2147483647 || isNaN(t)) {
																			let e = {};
																			u === null ? u = [e] : u.push(e), d++;
																		} else if (t < -2147483648 || isNaN(t)) {
																			let e = {};
																			u === null ? u = [e] : u.push(e), d++;
																		}
																	}
																	var _ = n === d;
																} else var _ = !0;
															}
														}
													}
												}
											}
										} else {
											let e = {};
											u === null ? u = [e] : u.push(e), d++;
										}
									}
									if (o !== d) break;
								}
							} else {
								let e = {};
								u === null ? u = [e] : u.push(e), d++;
							}
						}
						var y = a === d;
					} else var y = !0;
					if (y) {
						if (e.month !== void 0) {
							let t = e.month, n = d;
							if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
								let e = {};
								u === null ? u = [e] : u.push(e), d++;
							}
							if (d === n && typeof t == "number" && isFinite(t)) {
								if (t > 2147483647 || isNaN(t)) {
									let e = {};
									u === null ? u = [e] : u.push(e), d++;
								} else if (t < -2147483648 || isNaN(t)) {
									let e = {};
									u === null ? u = [e] : u.push(e), d++;
								}
							}
							var y = n === d;
						} else var y = !0;
						if (y) {
							if (e.status !== void 0) {
								let t = e.status, n = d;
								if (typeof t != "string") {
									let e = {};
									u === null ? u = [e] : u.push(e), d++;
								}
								if (t !== "ok") {
									let e = {};
									u === null ? u = [e] : u.push(e), d++;
								}
								var y = n === d;
							} else var y = !0;
							if (y) {
								if (e.year !== void 0) {
									let t = e.year, n = d;
									if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
										let e = {};
										u === null ? u = [e] : u.push(e), d++;
									}
									if (d === n && typeof t == "number" && isFinite(t)) {
										if (t > 2147483647 || isNaN(t)) {
											let e = {};
											u === null ? u = [e] : u.push(e), d++;
										} else if (t < -2147483648 || isNaN(t)) {
											let e = {};
											u === null ? u = [e] : u.push(e), d++;
										}
									}
									var y = n === d;
								} else var y = !0;
							}
						}
					}
				}
			}
		} else {
			let e = {};
			u === null ? u = [e] : u.push(e), d++;
		}
	}
	if (g === d) {
		let e = {};
		u === null ? u = [e] : u.push(e), d++;
	} else d = h, u !== null && (h ? u.length = h : u = null);
	return m === d ? (i.errors = [{
		instancePath: a,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (d = p, u !== null && (p ? u.length = p : u = null), i.errors = u, d === 0);
}
i.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
export { e as t };
