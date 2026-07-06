export type ACLMode = "whitelist" | "blacklist";

export type {
	AddRoomRequest,
	JoinedRoom,
	JoinedRoomsResponse,
	RemoveRoomRequest,
	RoomsResponse,
	SetAclRequest as SetACLRequest,
	SetAclResponse as SetACLResponse,
} from "@/api/generated/data-contracts";
