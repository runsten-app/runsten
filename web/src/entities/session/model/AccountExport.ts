import type { components } from '@/shared/api'

// AccountExport is how the account's ZIP archive stands: pending, running, or over;
// held once built, replaced by the next one asked for.
export type AccountExport = components['schemas']['AccountExport']

// isActive tells a job the export worker has still to build.
export const isActive = (job: AccountExport | null | undefined) =>
  job?.status === 'pending' || job?.status === 'running'

export const accountExportKeys = {
  job: () => ['account-export'] as const,
}
