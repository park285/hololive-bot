import { useSSRData } from '@/hooks/useSSRData'
import { DockerContainersSection } from '@/features/settings/components/DockerContainersSection'
import { SettingsFormSection } from '@/features/settings/components/SettingsFormSection'
import type { SettingsResponse } from '@/features/settings/types'

export const SettingsPage = () => {
  const ssrSettingsData = useSSRData('settings', (data) =>
    data?.status === 'ok' && data.settings ? (data as SettingsResponse) : undefined,
  )

  const ssrDockerHealthData = useSSRData('docker', (data) =>
    data?.status === 'ok' ? { status: data.status, available: data.available } : undefined,
  )

  const ssrContainersData = useSSRData('containers', (data) =>
    data?.status === 'ok' && data.containers
      ? { status: data.status, containers: data.containers }
      : undefined,
  )

  return (
    <div className="max-w-4xl mx-auto space-y-6">
      <SettingsFormSection initialData={ssrSettingsData} />
      <DockerContainersSection
        initialHealth={ssrDockerHealthData}
        initialContainers={ssrContainersData}
      />
    </div>
  )
}
