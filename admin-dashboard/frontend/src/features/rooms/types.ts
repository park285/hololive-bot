export type ACLMode = "whitelist" | "blacklist";

export type {
	AddRoomRequest,
	RemoveRoomRequest,
	RoomsResponse,
	SetAclRequest as SetACLRequest,
	SetAclResponse as SetACLResponse,
} from "@/api/generated/data-contracts";
