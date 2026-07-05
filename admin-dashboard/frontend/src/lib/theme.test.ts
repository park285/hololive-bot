import assert from "node:assert/strict";
import test from "node:test";
import {
	readStoredPreference,
	resolveTheme,
	storePreference,
} from "./theme";

const makeStorage = (overrides: Partial<Storage>): Storage => ({
	length: 0,
	clear: () => undefined,
	getItem: () => null,
	key: () => null,
	removeItem: () => undefined,
	setItem: () => undefined,
	...overrides,
});

const withLocalStorage = (storage: Storage, run: () => void): void => {
	const descriptor = Object.getOwnPropertyDescriptor(
		globalThis,
		"localStorage",
	);
	Object.defineProperty(globalThis, "localStorage", {
		value: storage,
		configurable: true,
		writable: true,
	});
	try {
		run();
	} finally {
		if (descriptor) {
			Object.defineProperty(globalThis, "localStorage", descriptor);
		} else {
			delete (globalThis as { localStorage?: Storage }).localStorage;
		}
	}
};

test("resolveTheme returns the explicit preference regardless of system", () => {
	assert.equal(resolveTheme("light", true), "light");
	assert.equal(resolveTheme("light", false), "light");
	assert.equal(resolveTheme("dark", true), "dark");
	assert.equal(resolveTheme("dark", false), "dark");
});

test("resolveTheme follows the system when preference is system", () => {
	assert.equal(resolveTheme("system", true), "dark");
	assert.equal(resolveTheme("system", false), "light");
});

test("readStoredPreference returns a valid stored preference", () => {
	withLocalStorage(makeStorage({ getItem: () => "dark" }), () => {
		assert.equal(readStoredPreference(), "dark");
	});
});

test("readStoredPreference falls back to system on invalid values", () => {
	withLocalStorage(makeStorage({ getItem: () => "purple" }), () => {
		assert.equal(readStoredPreference(), "system");
	});
	withLocalStorage(makeStorage({ getItem: () => null }), () => {
		assert.equal(readStoredPreference(), "system");
	});
});

test("readStoredPreference falls back to system when getItem throws", () => {
	withLocalStorage(
		makeStorage({
			getItem: () => {
				throw new Error("localStorage unavailable");
			},
		}),
		() => {
			assert.equal(readStoredPreference(), "system");
		},
	);
});

test("storePreference persists the preference", () => {
	let stored: string | null = null;
	withLocalStorage(
		makeStorage({
			setItem: (_key, value) => {
				stored = value;
			},
		}),
		() => {
			storePreference("dark");
		},
	);
	assert.equal(stored, "dark");
});

test("storePreference does not throw when setItem throws", () => {
	withLocalStorage(
		makeStorage({
			setItem: () => {
				throw new Error("quota exceeded");
			},
		}),
		() => {
			assert.doesNotThrow(() => {
				storePreference("light");
			});
		},
	);
});
