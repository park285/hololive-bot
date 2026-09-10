var e = t;
function t(e, { instancePath: n = "", parentData: r, parentDataProperty: i, rootData: a = e, dynamicAnchors: o = {} } = {}) {
	let s = null, c = 0, l = t.evaluated;
	l.dynamicProps && (l.props = void 0), l.dynamicItems && (l.items = void 0);
	let u = c, d = c, f = c, p = c;
	if (c === p) {
		if (e && typeof e == "object" && !Array.isArray(e)) {
			if (e.status === void 0) {
				let e = {};
				s === null ? s = [e] : s.push(e), c++;
			} else {
				let t = c;
				for (let t in e) if (t !== "message" && t !== "status") {
					let e = {};
					s === null ? s = [e] : s.push(e), c++;
					break;
				}
				if (t === c) {
					if (e.message !== void 0) {
						let t = e.message, n = c;
						if (typeof t != "string" && t !== null) {
							let e = {};
							s === null ? s = [e] : s.push(e), c++;
						}
						var m = n === c;
					} else var m = !0;
					if (m) {
						if (e.status !== void 0) {
							let t = e.status, n = c;
							if (typeof t != "string") {
								let e = {};
								s === null ? s = [e] : s.push(e), c++;
							}
							if (t !== "ok") {
								let e = {};
								s === null ? s = [e] : s.push(e), c++;
							}
							var m = n === c;
						} else var m = !0;
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
	return d === c ? (t.errors = [{
		instancePath: n,
		schemaPath: "#/not",
		keyword: "not",
		params: {}
	}], !1) : (c = u, s !== null && (u ? s.length = u : s = null), t.errors = s, c === 0);
}
t.evaluated = {
	dynamicProps: !1,
	dynamicItems: !1
};
export { e as t };
