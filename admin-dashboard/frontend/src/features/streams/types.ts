export type StreamOrg =
	| "hololive"
	| "vspo"
	| "stellive"
	| "independents"
	| "all";

export type {
	Stream,
	StreamsResponse,
} from "@/api/generated/data-contracts";
