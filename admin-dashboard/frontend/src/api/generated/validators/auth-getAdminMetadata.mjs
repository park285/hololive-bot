import { n as e, t } from "./shared-D64PDuVA.mjs";
import { t as n } from "./shared-CDcKZGHg.mjs";
var r = i;
function i(e, { instancePath: t = "", parentData: n, parentDataProperty: r, rootData: a = e, dynamicAnchors: o = {} } = {}) {
	let s = null, c = 0, l = i.evaluated;
	l.dynamicProps && (l.props = void 0), l.dynamicItems && (l.items = void 0);
	let u = c, d = c, f = c, p = c;
	if (c === p) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.path === void 0 || e.query === void 0 || e.headers === void 0) {
				let e = {};
				s === null ? s = [e] : s.push(e), c++;
			} else {
				let t = c;
				for (let t in e) if (t !== "path" && t !== "query" && t !== "headers" && t !== "body") {
					let e = {};
					s === null ? s = [e] : s.push(e), c++;
					break;
				}
				if (t === c) {
					if (e.path !== void 0) {
						let t = e.path, n = c;
						if (c === n) {
							if (t && typeof t == "object" && !Array.isArray(t)) for (let e in t) {
								let e = {};
								s === null ? s = [e] : s.push(e), c++;
								break;
							}
							else {
								let e = {};
								s === null ? s = [e] : s.push(e), c++;
							}
						}
						var m = n === c;
					} else var m = !0;
					if (m) {
						if (e.query !== void 0) {
							let t = e.query, n = c;
							if (c === n) {
								if (t && typeof t == "object" && !Array.isArray(t)) for (let e in t) {
									let e = {};
									s === null ? s = [e] : s.push(e), c++;
									break;
								}
								else {
									let e = {};
									s === null ? s = [e] : s.push(e), c++;
								}
							}
							var m = n === c;
						} else var m = !0;
						if (m) {
							if (e.headers !== void 0) {
								let t = e.headers, n = c;
								if (c === n) {
									if (t && typeof t == "object" && !Array.isArray(t)) for (let e in t) {
										let e = {};
										s === null ? s = [e] : s.push(e), c++;
										break;
									}
									else {
										let e = {};
										s === null ? s = [e] : s.push(e), c++;
									}
								}
								var m = n === c;
							} else var m = !0;
							if (m) {
								if (e.body !== void 0) {
									var m = !1;
									let e = {};
									s === null ? s = [e] : s.push(e), c++;
								} else var m = !0;
							}
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
	return d === c ? (i.errors = [{
		instancePath: t,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (c = u, s !== null && (u ? s.length = u : s = null), i.errors = s, c === 0);
}
i.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
function a(t) {
	return e(t) && n(t.body);
}
export { r as request_getAdminMetadata, a as response_09b0c71e1a33e540, t as response_b7a2836f983b1f2d };
