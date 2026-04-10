import { Admin } from "@/api/generated/Admin";
import apiClient from "@/api/client";

export const adminClient = new Admin();
adminClient.instance = apiClient;
