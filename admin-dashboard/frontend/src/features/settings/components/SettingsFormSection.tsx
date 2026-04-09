import type { ComponentProps } from 'react'
import { SettingsForm } from '@/components/settings/SettingsForm'

type SettingsFormSectionProps = ComponentProps<typeof SettingsForm>

export const SettingsFormSection = (props: SettingsFormSectionProps) => (
  <SettingsForm {...props} />
)
