import type { EditWar } from '../types';

let permissionGranted = false;

/**
 * Request browser notification permission.
 * Safe to call multiple times; only prompts once.
 */
export async function requestNotificationPermission(): Promise<boolean> {
  if (!('Notification' in window)) return false;

  if (Notification.permission === 'granted') {
    permissionGranted = true;
    return true;
  }

  if (Notification.permission === 'denied') {
    return false;
  }

  try {
    const result = await Notification.requestPermission();
    permissionGranted = result === 'granted';
    return permissionGranted;
  } catch {
    return false;
  }
}

/** Whether browser notifications are currently allowed. */
export function isNotificationPermitted(): boolean {
  if (!('Notification' in window)) return false;
  return Notification.permission === 'granted';
}

/**
 * Show a browser notification for a new edit war.
 * Clicking the notification brings focus back to the page.
 */
export function showEditWarNotification(war: EditWar): void {
  if (!isNotificationPermitted()) return;

  const severityEmoji: Record<string, string> = {
    critical: '🔴',
    high: '🟠',
    medium: '🟡',
    low: '🔵',
  };

  const emoji = severityEmoji[war.severity.toLowerCase()] ?? '⚔️';

  try {
    const notification = new Notification(`${emoji} Edit War Detected`, {
      body: `${war.page_title}\n${war.editors.length} editors · ${war.revert_count} reverts · Severity: ${war.severity}`,
      icon: '/favicon.ico',
      tag: `edit-war-${war.page_title}`,
      requireInteraction: war.severity.toLowerCase() === 'critical',
    });

    notification.onclick = () => {
      window.focus();
      notification.close();
    };

    // Auto close after 10 seconds for non-critical
    if (war.severity.toLowerCase() !== 'critical') {
      setTimeout(() => notification.close(), 10_000);
    }
  } catch {
    // Fallback: do nothing (in-app alert already handled in component)
  }
}

/**
 * Show a generic browser notification.
 */
export function showNotification(title: string, body: string): void {
  if (!isNotificationPermitted()) return;

  try {
    const notification = new Notification(title, {
      body,
      icon: '/favicon.ico',
    });
    notification.onclick = () => {
      window.focus();
      notification.close();
    };
    setTimeout(() => notification.close(), 8_000);
  } catch {
    // noop
  }
}
