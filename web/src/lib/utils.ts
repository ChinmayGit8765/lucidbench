import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/** True on Apple platforms, where the command key is shown instead of Ctrl. */
export const isMac = () => /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent)
