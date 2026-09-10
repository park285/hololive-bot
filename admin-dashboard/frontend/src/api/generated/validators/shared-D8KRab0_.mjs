import { n as e } from "./shared-t8Mukws9.mjs";
var t = r, n = e().default;
function r(e, { instancePath: t = "", parentData: i, parentDataProperty: a, rootData: o = e, dynamicAnchors: s = {} } = {}) {
	let c = null, l = 0, u = r.evaluated;
	u.dynamicProps && (u.props = void 0), u.dynamicItems && (u.items = void 0);
	let d = l, f = l, p = l, m = l;
	if (l === m) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.status === void 0 || e.message === void 0 || e.csrf_token === void 0) {
				let e = {};
				c === null ? c = [e] : c.push(e), l++;
			} else {
				let t = l;
				for (let t in e) if (t !== "csrf_token" && t !== "message" && t !== "status") {
					let e = {};
					c === null ? c = [e] : c.push(e), l++;
					break;
				}
				if (t === l) {
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
						if (e.message !== void 0) {
							let t = l;
							if (typeof e.message != "string") {
								let e = {};
								c === null ? c = [e] : c.push(e), l++;
							}
							var h = t === l;
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
