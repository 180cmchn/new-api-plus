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
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useAuthStore } from '@/stores/auth-store'

import { CommissionTransfersTable } from './components/commission-transfers-table'
import { ReferralTable } from './components/referral-table'
import { UsersDeleteDialog } from './components/users-delete-dialog'
import { UsersMutateDrawer } from './components/users-mutate-drawer'
import { UsersPrimaryButtons } from './components/users-primary-buttons'
import { UsersProvider, useUsers } from './components/users-provider'
import { UsersTable } from './components/users-table'

function UsersContent() {
  const { t } = useTranslation()
  const isRoot = useAuthStore((state) => state.auth.user?.role === 100)
  const { open, setOpen, currentRow } = useUsers()

  return (
    <>
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>{t('Users')}</SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <UsersPrimaryButtons />
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <Tabs defaultValue='users' className='flex min-h-0 flex-1 flex-col'>
            <TabsList>
              <TabsTrigger value='users'>{t('Users')}</TabsTrigger>
              {isRoot && (
                <TabsTrigger value='referrals'>
                  {t('Referral details')}
                </TabsTrigger>
              )}
              {isRoot && (
                <TabsTrigger value='transfers'>
                  {t('Commission transfers')}
                </TabsTrigger>
              )}
            </TabsList>
            <TabsContent value='users' className='flex min-h-0 flex-1 flex-col'>
              <UsersTable />
            </TabsContent>
            {isRoot && (
              <TabsContent
                value='referrals'
                className='flex min-h-0 flex-1 flex-col'
              >
                <ReferralTable />
              </TabsContent>
            )}
            {isRoot && (
              <TabsContent
                value='transfers'
                className='flex min-h-0 flex-1 flex-col'
              >
                <CommissionTransfersTable />
              </TabsContent>
            )}
          </Tabs>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <UsersMutateDrawer
        open={open === 'create' || open === 'update'}
        onOpenChange={(isOpen) => !isOpen && setOpen(null)}
        currentRow={open === 'update' ? currentRow || undefined : undefined}
      />
      <UsersDeleteDialog />
    </>
  )
}

export function Users() {
  return (
    <UsersProvider>
      <UsersContent />
    </UsersProvider>
  )
}
