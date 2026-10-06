import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/** True on Apple platforms, where the command key is shown instead of Ctrl. */
export const isMac = () => /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent)

/** "1 card", "2 cards": the count with its noun in the right number. Pass the plural for irregular nouns. */
export const plural = (n: number, word: string, many = `${word}s`) => `${n} ${n === 1 ? word : many}`
