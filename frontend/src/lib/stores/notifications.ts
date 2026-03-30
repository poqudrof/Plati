import { writable } from 'svelte/store';

export interface Notification {
  id: number;
  type: 'success' | 'error' | 'info';
  message: string;
}

let nextId = 0;
export const notifications = writable<Notification[]>([]);

export function addNotification(type: Notification['type'], message: string, duration = 5000) {
  const id = nextId++;
  notifications.update(n => [...n, { id, type, message }]);
  if (duration > 0) {
    setTimeout(() => {
      notifications.update(n => n.filter(x => x.id !== id));
    }, duration);
  }
}

export function removeNotification(id: number) {
  notifications.update(n => n.filter(x => x.id !== id));
}
