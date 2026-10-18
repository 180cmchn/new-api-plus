/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

import { changeInviter } from '../referral-api'
import type { User } from '../types'

export function InviterDialog(props: {
  user: User
  onClose: () => void
  onSuccess: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const schema = z.object({
    inviterId: z
      .number()
      .int()
      .positive(t('Invalid inviter'))
      .refine((id) => id !== props.user.id, t('Invalid inviter')),
  })
  const form = useForm({
    resolver: zodResolver(schema),
    defaultValues: { inviterId: props.user.inviter_id ?? 0 },
  })
  const mutation = useMutation({
    mutationFn: (values: z.infer<typeof schema>) =>
      changeInviter(props.user.id, values.inviterId),
    onSuccess: () => {
      toast.success(t('Inviter updated'))
      void client.invalidateQueries({ queryKey: ['referrals'] })
      void client.invalidateQueries({ queryKey: ['referral-summary'] })
      props.onSuccess()
      props.onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => {
        if (!open && !mutation.isPending) props.onClose()
      }}
      title={t('Bind or change inviter')}
      desc={t(
        'Change the inviter for {{username}}. Past consumption and rewards keep their original owner. Invitation counts follow the current binding.',
        { username: props.user.username }
      )}
      confirmText={t('Save')}
      isLoading={mutation.isPending}
      handleConfirm={form.handleSubmit((values) => mutation.mutate(values))}
    >
      <Form {...form}>
        <form onSubmit={form.handleSubmit((values) => mutation.mutate(values))}>
          <FormField
            control={form.control}
            name='inviterId'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Inviter user ID')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={1}
                    step={1}
                    {...field}
                    onChange={(event) =>
                      field.onChange(event.target.valueAsNumber)
                    }
                    disabled={mutation.isPending}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </form>
      </Form>
    </ConfirmDialog>
  )
}
