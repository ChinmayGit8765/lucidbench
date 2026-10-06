export const REPO = 'https://github.com/ChinmayGit8765/lucidbench';
export const RELEASES = `${REPO}/releases`;
export const DOCS = `${REPO}/blob/main/docs`;

// Internal URL helper that respects the configured base path.
export function url(path: string): string {
  const base = import.meta.env.BASE_URL.replace(/\/$/, '');
  return `${base}/${path}`;
}
