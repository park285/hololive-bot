import type { SettingsResponse } from '@/types'
import { useSSRData } from '@/hooks/useSSRData'
import { SettingsForm } from '@/components/settings/SettingsForm'
import { DockerContainerList } from '@/components/settings/DockerContainerList'

const SettingsTab = () => {
    // SSR 데이터 소비 (useSSRData 훅 활용)
    const ssrSettingsData = useSSRData('settings', (data) =>
        data?.status === 'ok' && data.settings ? (data as SettingsResponse) : undefined
    )

    const ssrDockerHealthData = useSSRData('docker', (data) =>
        data?.status === 'ok' ? { status: data.status, available: data.available } : undefined
    )

    const ssrContainersData = useSSRData('containers', (data) =>
        data?.status === 'ok' && data.containers
            ? { status: data.status, containers: data.containers }
            : undefined
    )

    return (
        <div className="max-w-4xl mx-auto space-y-6">
            <SettingsForm initialData={ssrSettingsData} />
            <DockerContainerList
                initialHealth={ssrDockerHealthData}
                initialContainers={ssrContainersData}
            />
        </div>
    )
}

export default SettingsTab

