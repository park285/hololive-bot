import type { Member } from "@/features/members/types";

export function cloneMembers(members: Member[]): Member[] {
	return members.map((member) => ({
		...member,
		aliases: {
			ko: [...member.aliases.ko],
			ja: [...member.aliases.ja],
		},
	}));
}

export function filterMembers(
	members: Member[],
	keyword: string,
	hideGraduated: boolean,
): Member[] {
	const normalized = keyword.trim().toLowerCase();
	return members.filter((member) => {
		if (hideGraduated && member.isGraduated) {
			return false;
		}
		if (!normalized) {
			return true;
		}
		return (
			member.name.toLowerCase().includes(normalized) ||
			member.channelId.toLowerCase().includes(normalized) ||
			String(member.id).includes(normalized) ||
			member.aliases.ko.some((alias) =>
				alias.toLowerCase().includes(normalized),
			) ||
			member.aliases.ja.some((alias) =>
				alias.toLowerCase().includes(normalized),
			)
		);
	});
}

export function sortMembers(members: Member[]): Member[] {
	return [...members].sort((first, second) => {
		if (first.isGraduated !== second.isGraduated) {
			return first.isGraduated ? 1 : -1;
		}
		return first.name.localeCompare(second.name);
	});
}
