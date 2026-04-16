import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const dirname = path.dirname(fileURLToPath(import.meta.url));
const membersPageSource = readFileSync(
	path.join(dirname, "pages", "MembersPage.tsx"),
	"utf8",
);
const membersToolbarSource = readFileSync(
	path.join(dirname, "components", "MembersToolbar.tsx"),
	"utf8",
);

test("MembersPage lazy-loads modal bundles instead of statically importing them", () => {
	assert.equal(membersPageSource.includes('import AddMemberModal'), false);
	assert.equal(membersPageSource.includes('import ChannelEditModal'), false);
	assert.equal(membersPageSource.includes('import EditNameModal'), false);
	assert.equal(membersPageSource.includes('import { ConfirmModal }'), false);

	assert.match(
		membersPageSource,
		/const loadAddMemberModal = \(\) => import\("@\/components\/AddMemberModal"\);/,
	);
	assert.match(
		membersPageSource,
		/const loadChannelEditModal = \(\) => import\("@\/components\/ChannelEditModal"\);/,
	);
	assert.match(
		membersPageSource,
		/const loadEditNameModal = \(\) => import\("@\/components\/EditNameModal"\);/,
	);
	assert.match(
		membersPageSource,
		/const loadConfirmModal = \(\) =>[\s\S]*import\("@\/components\/ConfirmModal"\)/,
	);
});

test("Members toolbar preloads the add-member modal on user intent", () => {
	assert.match(membersToolbarSource, /onMouseEnter=\{onAddModalIntent\}/);
	assert.match(membersToolbarSource, /onFocus=\{onAddModalIntent\}/);
});
