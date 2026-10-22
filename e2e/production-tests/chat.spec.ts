import { expect, test } from '@playwright/test';
test('production support actions open the quick chat without leaving the page', async ({ page }) => {
  await page.goto('/contacts'); await expect(page.locator('.store-footer-chat')).toHaveAttribute('href', '/chat');
  await page.locator('.chat-launcher').click();await expect(page).toHaveURL(/\/contacts$/);
  const dialog=page.getByRole('dialog',{name:'Быстрый чат с Фикусином'});await expect(dialog).toBeVisible();
  await expect(dialog.getByText('Загружаем переписку…',{exact:true})).toBeHidden();await expect(dialog.getByRole('alert')).toHaveCount(0);
  await expect(dialog.locator('.chat-avatar')).toHaveAttribute('src','/icon-192.png');
  await expect(dialog.getByRole('link',{name:'Вся переписка в личном кабинете'})).toHaveCount(0);
  await expect(dialog.getByText('История переписки — в',{exact:false})).toHaveCount(0);
  await dialog.getByRole('textbox',{name:'Сообщение',exact:true}).fill('Проверка формы без отправки');await expect(dialog.getByRole('button',{name:'Отправить',exact:true})).toBeDisabled();
  await dialog.getByRole('checkbox').check();await expect(dialog.getByRole('button',{name:'Отправить',exact:true})).toBeEnabled();
  const bounds=await dialog.getByRole('button',{name:'Отправить',exact:true}).boundingBox();expect(bounds!.y+bounds!.height).toBeLessThanOrEqual(page.viewportSize()!.height+1);
  expect(await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth)).toBeLessThanOrEqual(1);
  const thread=await page.request.get('/api/v1/chat');expect(thread.status()).toBe(200);expect(thread.headers()['cache-control']).toBe('no-store');expect((await thread.json()).messages).toEqual([]);
  await dialog.getByRole('button',{name:'Свернуть чат',exact:true}).click();await expect(dialog).toBeHidden();
  // Do not submit synthetic messages to production.
});
