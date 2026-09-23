import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

// cn merges Tailwind CSS class names with deduplication.
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
