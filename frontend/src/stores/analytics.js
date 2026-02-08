import { get } from 'svelte/store';
import { apiBase } from '../lib/api.js';
import { settings } from './settings.js';

function isAnalyticsEnabled() {
  return get(settings).analytics !== false;
}

export function getTrackingId() {
  let trackingId = localStorage.getItem('yoink_trackingId');
  if (!trackingId) {
    trackingId = 'tid-' + Date.now() + '-' + Math.random().toString(36).slice(2, 18);
    localStorage.setItem('yoink_trackingId', trackingId);
  }
  return trackingId;
}

export function clearTrackingId() {
  localStorage.removeItem('yoink_trackingId');
  localStorage.removeItem('yoink_last_daily_report');
}

export async function deleteUserAnalyticsData() {
  const trackingId = localStorage.getItem('yoink_trackingId');
  if (!trackingId) return { deleted: false };

  try {
    const res = await fetch(`${apiBase()}/api/analytics/delete`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ trackingId }),
    });
    const result = await res.json();
    if (result.deleted) {
      clearTrackingId();
    }
    return result;
  } catch (e) {
    console.error('Failed to delete analytics data:', e);
    return { deleted: false, error: e.message };
  }
}

export async function reportPageView(page) {
  if (!isAnalyticsEnabled()) return;

  try {
    await fetch(`${apiBase()}/api/analytics/track`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ type: 'pageview', page, trackingId: getTrackingId() }),
    });
  } catch {}
}

export async function reportDailyUser() {
  if (!isAnalyticsEnabled()) return;

  const today = new Date().toISOString().split('T')[0];
  const lastReport = localStorage.getItem('yoink_last_daily_report');
  if (lastReport === today) return;

  try {
    await fetch(`${apiBase()}/api/analytics/track`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ type: 'dailyUser', trackingId: getTrackingId() }),
    });
    localStorage.setItem('yoink_last_daily_report', today);
  } catch {}
}
