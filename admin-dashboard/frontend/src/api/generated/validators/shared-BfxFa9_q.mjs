var e = n, t = /* @__PURE__ */ RegExp("^[a-f0-9]{64}$", "u");
function n(e, { instancePath: r = "", parentData: i, parentDataProperty: a, rootData: o = e, dynamicAnchors: s = {} } = {}) {
	let c = null, l = 0, u = n.evaluated;
	u.dynamicProps && (u.props = void 0), u.dynamicItems && (u.items = void 0);
	let d = l, f = l, p = l, m = l;
	if (l === m) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.path === void 0 || e.query === void 0 || e.headers === void 0) {
				let e = {};
				c === null ? c = [e] : c.push(e), l++;
			} else {
				let n = l;
				for (let t in e) if (t !== "path" && t !== "query" && t !== "headers" && t !== "body") {
					let e = {};
					c === null ? c = [e] : c.push(e), l++;
					break;
				}
				if (n === l) {
					if (e.path !== void 0) {
						let t = e.path, n = l;
						if (l === n) {
							if (t && typeof t == "object" && !Array.isArray(t)) for (let e in t) {
								let e = {};
								c === null ? c = [e] : c.push(e), l++;
								break;
							}
							else {
								let e = {};
								c === null ? c = [e] : c.push(e), l++;
							}
						}
						var h = n === l;
					} else var h = !0;
					if (h) {
						if (e.query !== void 0) {
							let t = e.query, n = l;
							if (l === n) {
								if (t && typeof t == "object" && !Array.isArray(t)) for (let e in t) {
									let e = {};
									c === null ? c = [e] : c.push(e), l++;
									break;
								}
								else {
									let e = {};
									c === null ? c = [e] : c.push(e), l++;
								}
							}
							var h = n === l;
						} else var h = !0;
						if (h) {
							if (e.headers !== void 0) {
								let n = e.headers, r = l;
								if (l === r) {
									if (n && typeof n == "object" && !Array.isArray(n)) {
										if (n["x-admin-client-generation"] === void 0) {
											let e = {};
											c === null ? c = [e] : c.push(e), l++;
										} else {
											let e = l;
											for (let e in n) if (e !== "x-admin-client-generation") {
												let e = {};
												c === null ? c = [e] : c.push(e), l++;
												break;
											}
											if (e === l && n["x-admin-client-generation"] !== void 0) {
												let e = n["x-admin-client-generation"];
												if (l === l) {
													if (typeof e == "string") {
														if (!t.test(e)) {
															let e = {};
															c === null ? c = [e] : c.push(e), l++;
														}
													} else {
														let e = {};
														c === null ? c = [e] : c.push(e), l++;
													}
												}
											}
										}
									} else {
										let e = {};
										c === null ? c = [e] : c.push(e), l++;
									}
								}
								var h = r === l;
							} else var h = !0;
							if (h) {
								if (e.body !== void 0) {
									var h = !1;
									let e = {};
									c === null ? c = [e] : c.push(e), l++;
								} else var h = !0;
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
	return f === l ? (n.errors = [{
		instancePath: r,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (l = d, c !== null && (d ? c.length = d : c = null), n.errors = c, l === 0);
}
n.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
export { e as t };
