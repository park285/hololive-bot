import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";
export function cn(...inputs: ClassValue[]): string {
	return twMerge(clsx(inputs));
}

export function unixToDate(unixSeconds: number): Date {
	return new Date(unixSeconds * 1000);
}

export function unixToMs(unixSeconds: number): number {
	return unixSeconds * 1000;
}

export function dateToUnix(date: Date): number {
	return Math.floor(date.getTime() / 1000);
}

export function getRemainingMs(unixSeconds: number): number {
	return unixSeconds * 1000 - Date.now();
}

export function getRemainingMinutes(unixSeconds: number): number {
	return Math.floor(getRemainingMs(unixSeconds) / 1000 / 60);
}
