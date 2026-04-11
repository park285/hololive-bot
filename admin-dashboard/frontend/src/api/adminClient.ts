import { Admin } from "@/api/generated/Admin";
import { createApiClient } from "@/api/client";

export const adminClient = new Admin();
adminClient.instance = createApiClient("");
