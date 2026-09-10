import { SessionAbsoluteWarningModal } from "@/components/auth/SessionAbsoluteWarningModal";
import { SessionIdleWarningModal } from "@/components/auth/SessionIdleWarningModal";

/** SessionWarningDialogs는 첫 만료 경고가 필요할 때 가져오는 기존 알림 UI입니다. */
export default function SessionWarningDialogs() {
	return <><SessionIdleWarningModal /><SessionAbsoluteWarningModal /></>;
}
