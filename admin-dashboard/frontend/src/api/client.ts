import axios from "axios";
import { Admin } from "@/api/generated/Admin";
import { createSDKTransport, type TransportPolicy } from "@/api/transport";

/** createAdminClient는 앱이 주입한 정책과 단일 HTTP transport로 SDK를 구성합니다. */
export function createAdminClient(baseURL: string, timeoutMs: number, policy: TransportPolicy) {
	const http = axios.create({ baseURL, withCredentials: true, headers: { "Content-Type": "application/json" }, timeout: timeoutMs });
	return { http, sdk: new Admin(createSDKTransport(http, policy)) };
}
