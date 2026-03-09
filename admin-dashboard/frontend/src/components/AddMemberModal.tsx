import { useEffect, useMemo, useState, type SyntheticEvent } from 'react'
import Save from 'lucide-react/dist/esm/icons/save'
import UserPlus from 'lucide-react/dist/esm/icons/user-plus'
import { BaseModal } from '@/components/ui/BaseModal'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Label } from '@/components/ui/Label'

interface AddMemberFormValues {
  name: string
  channelId: string
  nameKo?: string
  nameJa?: string
}

interface AddMemberModalProps {
  isOpen: boolean
  onClose: () => void
  onAdd: (member: AddMemberFormValues) => void
}

interface AddMemberErrors {
  name?: string
  channelId?: string
}

const initialValues: AddMemberFormValues = {
  name: '',
  channelId: '',
  nameKo: '',
  nameJa: '',
}

const validate = (values: AddMemberFormValues): AddMemberErrors => {
  const errors: AddMemberErrors = {}

  if (!values.name.trim()) {
    errors.name = '멤버 이름을 입력해주세요.'
  }

  if (values.channelId.trim().length < 24) {
    errors.channelId = 'ID 형식이 올바르지 않습니다 (최소 24자).'
  }

  return errors
}

export default function AddMemberModal({
  isOpen,
  onClose,
  onAdd,
}: AddMemberModalProps) {
  const [values, setValues] = useState<AddMemberFormValues>(initialValues)
  const [errors, setErrors] = useState<AddMemberErrors>({})

  useEffect(() => {
    if (isOpen) {
      setValues(initialValues)
      setErrors({})
    }
  }, [isOpen])

  const isDirty = useMemo(
    () => [values.name, values.channelId, values.nameKo ?? '', values.nameJa ?? '']
      .some((value) => value.trim() !== ''),
    [values],
  )

  const handleChange = (field: keyof AddMemberFormValues, value: string) => {
    setValues((prev) => ({ ...prev, [field]: value }))
    setErrors((prev) => ({ ...prev, [field]: undefined }))
  }

  const handleSubmit = (event: SyntheticEvent<HTMLFormElement>) => {
    event.preventDefault()

    const nextErrors = validate(values)
    if (Object.keys(nextErrors).length > 0) {
      setErrors(nextErrors)
      return
    }

    onAdd({
      name: values.name.trim(),
      channelId: values.channelId.trim(),
      nameKo: values.nameKo?.trim() ?? '',
      nameJa: values.nameJa?.trim() ?? '',
    })
    onClose()
  }

  const title = (
    <span className="flex items-center gap-2">
      <UserPlus className="text-sky-600" size={20} aria-hidden="true" />
      새 멤버 추가
    </span>
  )

  return (
    <BaseModal isOpen={isOpen} onClose={onClose} title={title} maxWidth="lg" showHeaderBorder>
      <form onSubmit={handleSubmit} className="space-y-4">
        <div className="space-y-2">
          <Label htmlFor="add-member-name">멤버 이름 (기본)</Label>
          <Input
            id="add-member-name"
            value={values.name}
            onChange={(event) => { handleChange('name', event.target.value) }}
            placeholder="예: Hoshimachi Suisei"
            className="focus-visible:ring-2 focus-visible:ring-sky-200"
            hasError={!!errors.name}
          />
          {errors.name && <p className="text-[0.8rem] font-medium text-destructive">{errors.name}</p>}
        </div>

        <div className="space-y-2">
          <Label htmlFor="add-member-channel-id">YouTube 채널 ID</Label>
          <Input
            id="add-member-channel-id"
            value={values.channelId}
            onChange={(event) => { handleChange('channelId', event.target.value) }}
            placeholder="UC…"
            className="font-mono focus-visible:ring-2 focus-visible:ring-sky-200"
            hasError={!!errors.channelId}
          />
          {errors.channelId && <p className="text-[0.8rem] font-medium text-destructive">{errors.channelId}</p>}
        </div>

        <div className="grid grid-cols-2 gap-4">
          <div className="space-y-2">
            <Label htmlFor="add-member-name-ko" className="text-slate-500">한국어 이름 (선택)</Label>
            <Input
              id="add-member-name-ko"
              value={values.nameKo ?? ''}
              onChange={(event) => { handleChange('nameKo', event.target.value) }}
              placeholder="예: 호시마치 스이세이"
              className="focus-visible:ring-2 focus-visible:ring-sky-200"
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="add-member-name-ja" className="text-slate-500">일본어 이름 (선택)</Label>
            <Input
              id="add-member-name-ja"
              value={values.nameJa ?? ''}
              onChange={(event) => { handleChange('nameJa', event.target.value) }}
              placeholder="예: 星街すいせい"
              className="focus-visible:ring-2 focus-visible:ring-sky-200"
            />
          </div>
        </div>

        <div className="mt-6 flex justify-end gap-3 pt-2">
          <Button
            type="button"
            variant="outline"
            onClick={onClose}
            className="focus-visible:ring-2 focus-visible:ring-slate-200"
          >
            취소
          </Button>
          <Button
            type="submit"
            disabled={!isDirty}
            className="gap-2 bg-sky-600 hover:bg-sky-700 shadow-sm shadow-sky-200 focus-visible:ring-2 focus-visible:ring-sky-200"
            aria-label="새 멤버 정보 저장 및 추가"
          >
            <Save size={16} aria-hidden="true" /> 추가하기
          </Button>
        </div>
      </form>
    </BaseModal>
  )
}
