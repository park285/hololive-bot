import { test, expect } from '@playwright/test';

test('YouTube Ops Page visual check', async ({ page }) => {
  // Go to the local login page first
  await page.goto('http://localhost:5173/login');
  
  // Perform login
  await page.fill('input[name="username"]', 'kapu');
  await page.fill('input[name="password"]', 'sktjtm290612!');
  await page.click('button[type="submit"]');
  
  // Wait for successful login redirect to dashboard
  await page.waitForURL('**/dashboard/**', { timeout: 10000 });
  
  // Navigate to YouTube Ops page locally
  await page.goto('http://localhost:5173/dashboard/youtube-ops');
  
  // Wait for the main heading to ensure page has loaded
  await expect(page.locator('h1', { hasText: 'YouTube 커뮤니티 및 쇼츠 운영 현황' })).toBeVisible({ timeout: 10000 });
  
  // Wait a bit to let the charts/data load fully
  await page.waitForTimeout(3000);
  
  // Take a full page screenshot
  await page.screenshot({ path: 'youtube-ops-page.png', fullPage: true });
});
