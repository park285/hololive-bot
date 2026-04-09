import type { ComponentProps } from 'react'
import { DockerContainerList } from '@/components/settings/DockerContainerList'

type DockerContainersSectionProps = ComponentProps<typeof DockerContainerList>

export const DockerContainersSection = (props: DockerContainersSectionProps) => (
  <DockerContainerList {...props} />
)
