import { useEffect, useMemo, useState, type SyntheticEvent } from 'react'
import AlertTriangle from 'lucide-react/dist/esm/icons/alert-triangle'
import Save from 'lucide-react/dist/esm/icons/save'
import { BaseModal } from '@/components/ui/BaseModal'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Label } from '@/components/ui/Label'

interface EditNameModalProps {
  isOpen: boolean
  onClose: () => void
  onSave: (newName: string) => void
  type: 'room' | 'user' | 'member'
  id: string
  currentName: string
}

export default function EditNameModal({
  isOpen,
  onClose,
  onSave,
  type,
  id,
  currentName,
}: EditNameModalProps) {
  const [name, setName] = useState(currentName)
  const [error, setError] = useState('')

  useEffect(() => {
    if (isOpen) {
      setName(currentName)
      setError('')
    }
  }, [currentName, isOpen])

  const isDirty = useMemo(() => name.trim() !== currentName.trim(), [currentName, name])

  const handleSubmit = (event: SyntheticEvent<HTMLFormElement>) => {
    event.preventDefault()

    if (!name.trim()) {
      setError('이름을 입력해주세요.')
      return
    }

    onSave(name.trim())
    onClose()
  }

  const getTitle = () => {
    switch (type) {
      case 'room':
        return '방 이름 수정'
      case 'user':
        return '사용자 이름 수정'
      case 'member':
        return '멤버 이름 수정'
      default:
        return '이름 수정'
    }
  }

  const showIdWarning = type !== 'member' && /^\d+$/.test(id)

  return (
    <BaseModal isOpen={isOpen} onClose={onClose} title={getTitle()} showHeaderBorder>
      <form onSubmit={handleSubmit} className="space-y-4">
        <div className="mb-4 rounded-lg border border-slate-100 bg-slate-50 p-3">
          <div className="mb-1 text-xs font-medium text-slate-500">ID (변경 불가)</div>
          <div className="text-sm font-mono text-slate-700">{id}</div>
        </div>

        {showIdWarning && (
          <div className="mb-4 flex items-start gap-2 rounded-lg border border-amber-100 bg-amber-50 p-3">
            <AlertTriangle size={16} className="mt-0.5 shrink-0 text-amber-500" />
            <div className="text-xs leading-snug text-amber-700">
              현재 ID가 사용 중입니다. 이름을 설정하면 ID 대신 이름이 표시됩니다.
            </div>
          </div>
        )}

        <div className="space-y-2">
          <Label htmlFor="edit-name-input">새로운 이름</Label>
          <Input
            id="edit-name-input"
            value={name}
            onChange={(event) => {
              setName(event.target.value)
              setError('')
            }}
            placeholder="이름을 입력하세요"
            hasError={!!error}
          />
          {error && <p className="text-[0.8rem] font-medium text-destructive">{error}</p>}
        </div>

        <div className="mt-6 flex justify-end gap-3 pt-2">
          <Button type="button" variant="outline" onClick={onClose}>
            취소
          </Button>
          <Button type="submit" disabled={!isDirty} className="gap-2">
            <Save size={16} aria-hidden="true" /> 저장
          </Button>
        </div>
      </form>
    </BaseModal>
  )
}
