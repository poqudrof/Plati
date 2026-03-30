import { writable } from 'svelte/store';
import type { User } from '$lib/api/types';

export const currentUser = writable<User | null>(null);
export const isLoading = writable(true);
