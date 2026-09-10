var e = n, t = /* @__PURE__ */ RegExp("^[a-f0-9]{64}$", "u");
function n(e, { instancePath: r = "", parentData: i, parentDataProperty: a, rootData: o = e, dynamicAnchors: s = {} } = {}) {
	let c = null, l = 0, u = n.evaluated;
	u.dynamicProps && (u.props = void 0), u.dynamicItems && (u.items = void 0);
	let d = l, f = l, p = l, m = l;
	if (l === m) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.clientGeneration === void 0) {
				let e = {};
				c === null ? c = [e] : c.push(e), l++;
			} else {
				let n = l;
				for (let t in e) if (t !== "clientGeneration") {
					let e = {};
					c === null ? c = [e] : c.push(e), l++;
					break;
				}
				if (n === l && e.clientGeneration !== void 0) {
					let n = e.clientGeneration;
					if (l === l) {
						if (typeof n == "string") {
							if (!t.test(n)) {
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
