import { t as e } from "./shared-t8Mukws9.mjs";
var t = r, n = /* @__PURE__ */ RegExp("^[a-f0-9]{64}$", "u");
function r(e, { instancePath: t = "", parentData: i, parentDataProperty: a, rootData: o = e, dynamicAnchors: s = {} } = {}) {
	let c = null, l = 0, u = r.evaluated;
	u.dynamicProps && (u.props = void 0), u.dynamicItems && (u.items = void 0);
	let d = l, f = l, p = l, m = l;
	if (l === m) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.headers === void 0 || e.body === void 0) {
				let e = {};
				c === null ? c = [e] : c.push(e), l++;
			} else {
				let t = l;
				for (let t in e) if (t !== "headers" && t !== "body") {
					let e = {};
					c === null ? c = [e] : c.push(e), l++;
					break;
				}
				if (t === l && e.headers !== void 0) {
					let t = e.headers;
					if (l === l) {
						if (t && typeof t == "object" && !Array.isArray(t)) {
							if (t["x-admin-server-generation"] === void 0) {
								let e = {};
								c === null ? c = [e] : c.push(e), l++;
							} else {
								let e = l;
								for (let e in t) if (e !== "x-admin-server-generation") {
									let e = {};
									c === null ? c = [e] : c.push(e), l++;
									break;
								}
								if (e === l && t["x-admin-server-generation"] !== void 0) {
									let e = t["x-admin-server-generation"];
									if (l === l) {
										if (typeof e == "string") {
											if (!n.test(e)) {
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
function i(n) {
	return t(n) && e(n.body);
}
export { t as n, i as t };
