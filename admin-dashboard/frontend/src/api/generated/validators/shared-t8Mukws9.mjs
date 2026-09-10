var e = /* @__PURE__ */ ((e, t) => () => (t || (e((t = { exports: {} }).exports, t), e = null), t.exports))(((e) => {
	Object.defineProperty(e, "__esModule", { value: !0 });
	function t(e) {
		let t = e.length, n = 0, r = 0, i;
		for (; r < t;) n++, i = e.charCodeAt(r++), i >= 55296 && i <= 56319 && r < t && (i = e.charCodeAt(r), (i & 64512) == 56320 && r++);
		return n;
	}
	e.default = t, t.code = "require(\"ajv/dist/runtime/ucs2length\").default";
})), t = a, n = /* @__PURE__ */ RegExp("^[A-Z][A-Z0-9_]*$", "u"), r = /* @__PURE__ */ RegExp("^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$", "u"), i = e().default;
function a(e, { instancePath: t = "", parentData: o, parentDataProperty: s, rootData: c = e, dynamicAnchors: l = {} } = {}) {
	let u = null, d = 0, f = a.evaluated;
	f.dynamicProps && (f.props = void 0), f.dynamicItems && (f.items = void 0);
	let p = d, m = d, h = d, g = d;
	if (d === g) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.code === void 0 || e.message === void 0 || e.requestId === void 0) {
				let e = {};
				u === null ? u = [e] : u.push(e), d++;
			} else {
				let t = d;
				for (let t in e) if (t !== "code" && t !== "message" && t !== "requestId" && t !== "absolute_expired" && t !== "retry_after" && t !== "notDispatchedMutationId") {
					let e = {};
					u === null ? u = [e] : u.push(e), d++;
					break;
				}
				if (t === d) {
					if (e.code !== void 0) {
						let t = e.code, r = d;
						if (d === r) {
							if (typeof t == "string") {
								if (!n.test(t)) {
									let e = {};
									u === null ? u = [e] : u.push(e), d++;
								}
							} else {
								let e = {};
								u === null ? u = [e] : u.push(e), d++;
							}
						}
						var _ = r === d;
					} else var _ = !0;
					if (_) {
						if (e.message !== void 0) {
							let t = d;
							if (typeof e.message != "string") {
								let e = {};
								u === null ? u = [e] : u.push(e), d++;
							}
							var _ = t === d;
						} else var _ = !0;
						if (_) {
							if (e.requestId !== void 0) {
								let t = e.requestId, n = d;
								if (d === n) {
									if (typeof t == "string") {
										if (i(t) < 1) {
											let e = {};
											u === null ? u = [e] : u.push(e), d++;
										}
									} else {
										let e = {};
										u === null ? u = [e] : u.push(e), d++;
									}
								}
								var _ = n === d;
							} else var _ = !0;
							if (_) {
								if (e.absolute_expired !== void 0) {
									let t = d;
									if (typeof e.absolute_expired != "boolean") {
										let e = {};
										u === null ? u = [e] : u.push(e), d++;
									}
									var _ = t === d;
								} else var _ = !0;
								if (_) {
									if (e.retry_after !== void 0) {
										let t = e.retry_after, n = d;
										if (!(typeof t == "number" && !(t % 1) && !isNaN(t) && isFinite(t))) {
											let e = {};
											u === null ? u = [e] : u.push(e), d++;
										}
										if (d === n && typeof t == "number" && isFinite(t) && (t < 0 || isNaN(t))) {
											let e = {};
											u === null ? u = [e] : u.push(e), d++;
										}
										var _ = n === d;
									} else var _ = !0;
									if (_) {
										if (e.notDispatchedMutationId !== void 0) {
											let t = e.notDispatchedMutationId, n = d;
											if (d === n) {
												if (typeof t == "string") {
													if (!r.test(t)) {
														let e = {};
														u === null ? u = [e] : u.push(e), d++;
													}
												} else {
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
	return m === d ? (a.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (d = p, u !== null && (p ? u.length = p : u = null), a.errors = u, d === 0);
}
a.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
export { e as n, t };
