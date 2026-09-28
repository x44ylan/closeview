// Run against a running CloseView preview with playwright-cli:
//   mkdir -p tmp/sidebar-check
//   playwright-cli -s=panels open http://127.0.0.1:4177
//   playwright-cli -s=panels run-code --filename=scripts/check-sidebars.js
//   playwright-cli -s=panels close
// API responses are synthetic; this check never reads or deletes native sessions.
// Failure cases covered: populated/missing token counters leaking into the UI;
// missing/wrong icons or visible button text; inaccessible toggle states;
// independent panels interfering; lost state on reload; lost resize widths;
// hidden resize handles still focusable; library views gaining an extra column;
// mobile overflow/navigation broken by saved desktop states; unavailable storage;
// and browser runtime errors. Screenshots are written under tmp/sidebar-check.
async page => {
  const base = await page.evaluate(() => location.origin);
  const errors = [];
  const check = (condition, message) => { if (!condition) throw new Error(message); };
  page.on('pageerror', error => errors.push(error.message));
  let recorded = true;
  const session = { id: 'codex.fixture', nativeId: 'fixture', threadId: 'fixture', source: 'codex', title: 'Sidebar review', projectPath: '/fixture', createdAt: '2026-09-28T00:00:00Z', updatedAt: '2026-09-28T00:00:00Z', model: 'fixture-model', provider: 'openai', agent: '', origin: '', isSubsession: false, childCount: 0, messageCount: 2 };
  await page.route('**/api/**', async route => {
    const pathname = '/api/' + route.request().url().split('/api/')[1].split('?')[0];
    check(route.request().method() === 'GET', 'Unexpected mutating API request');
    let data;
    if (pathname === '/api/sessions') data = { sessions: [session], sources: [{ name: 'codex', available: true, count: 1 }] };
    else if (pathname.startsWith('/api/sessions/')) data = {
      session, warnings: [], toolCalls: [], usage: recorded ? { input_tokens: 12000, output_tokens: 6000, cached_input_tokens: 3000, reasoning_output_tokens: 1000 } : null,
      messages: [
        { id: 'user-1', sequence: 1, role: 'user', content: 'Review the panel layout.', createdAt: session.createdAt },
        { id: 'assistant-1', sequence: 2, role: 'assistant', content: 'Both sidebars can be collapsed independently.', createdAt: session.updatedAt, model: 'fixture-model', tokensInput: recorded ? 12000 : 0, tokensOutput: recorded ? 6000 : 0, tokensReasoning: recorded ? 1000 : 0, tokensCacheRead: recorded ? 3000 : 0, tokensCacheWrite: recorded ? 2000 : 0, cost: recorded ? 1.25 : 0 },
      ],
    };
    else if (pathname === '/api/skills') data = { skills: [], sources: [] };
    else if (pathname === '/api/mcp') data = { servers: [], sources: [] };
    else throw new Error('Unexpected API endpoint: ' + pathname);
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(data) });
  });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(base);
  await page.evaluate(() => localStorage.clear());
  await page.reload();
  await page.getByText('Both sidebars can be collapsed independently.', { exact: true }).waitFor();
  const button = (side, collapsed) => page.getByRole('button', { name: `${collapsed ? 'Expand' : 'Collapse'} ${side} sidebar`, exact: true });
  const width = selector => page.locator(selector).evaluate(el => el.getBoundingClientRect().width);
  const columns = () => page.locator('.app-shell').evaluate(el => getComputedStyle(el).gridTemplateColumns.split(' ').length);
  const noCounters = async () => {
    check(await page.locator('.message-usage, .conversation-totals').count() === 0, 'Usage widgets remain');
    check(!/Token usage|Cache read|Cache write|Input\s+12|Output\s+6|\$1\.2500/i.test(await page.locator('body').innerText()), 'Usage labels remain');
  };
  await noCounters();
  for (const side of ['left', 'right']) {
    const toggle = button(side, false);
    check((await toggle.innerText()).trim() === '', `${side} toggle has visible text`);
    check(await toggle.getAttribute('aria-expanded') === 'true', `${side} toggle state missing`);
    check(await toggle.locator(`.tabler-icon-layout-sidebar-${side}-collapse`).count() === 1, `${side} collapse icon wrong`);
  }
  await page.screenshot({ path: 'tmp/sidebar-check/expanded.png' });
  const initialRight = await width('#prompts-panel');
  await button('left', false).click();
  check(await width('#sessions-panel') === 48, 'Left panel did not collapse');
  check(await width('#prompts-panel') === initialRight, 'Left collapse changed right panel');
  await button('right', false).click();
  check(await width('#prompts-panel') === 48, 'Right panel did not collapse');
  check(!await page.getByRole('separator', { name: 'Resize prompts panel' }).isVisible(), 'Collapsed resize handle remains visible');
  await page.screenshot({ path: 'tmp/sidebar-check/collapsed.png' });
  await page.reload();
  await button('right', true).waitFor();
  for (const side of ['left', 'right']) {
    check(await button(side, true).getAttribute('aria-expanded') === 'false', `${side} collapse not persisted`);
    check(await button(side, true).locator(`.tabler-icon-layout-sidebar-${side}-expand`).count() === 1, `${side} expand icon wrong`);
  }
  await button('right', true).focus();
  await page.keyboard.press('Enter');
  check(await width('#prompts-panel') > 48 && await width('#sessions-panel') === 48, 'Right keyboard expansion affected left');
  await button('left', true).click();
  const separator = page.getByRole('separator', { name: 'Resize prompts panel' });
  await separator.focus();
  await page.keyboard.press('ArrowLeft');
  const resizedRight = await width('#prompts-panel');
  check(resizedRight > initialRight, 'Right resize failed');
  await button('right', false).click();
  await button('right', true).click();
  check(await width('#prompts-panel') === resizedRight, 'Right resize width lost');
  await button('right', false).click();
  for (const tab of ['Skills', 'MCP']) {
    await page.getByRole('tab', { name: tab, exact: true }).click();
    check(await page.locator('#prompts-panel').count() === 0, 'Prompts panel leaked into library');
    check(await columns() === 2, 'Extra library grid column');
    await button('left', false).click();
    check(await width('#sessions-panel') === 48, 'Library sidebar did not collapse');
    await button('left', true).click();
  }
  await page.getByRole('tab', { name: 'Sessions', exact: true }).click();
  check(await width('#prompts-panel') === 48, 'Right state lost switching views');
  await page.setViewportSize({ width: 1000, height: 800 });
  check(!await page.locator('#prompts-panel').isVisible(), 'Prompts panel should hide at tablet width');
  check(await columns() === 2, 'Tablet has extra grid column');
  await button('left', false).click();
  check(await width('#sessions-panel') === 48, 'Tablet left collapse failed');
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: 'Open session navigation' }).click();
  await page.getByRole('tab', { name: 'Skills', exact: true }).waitFor({ state: 'visible' });
  check(await page.locator('#sessions-panel').evaluate(el => el.getBoundingClientRect().width) > 48, 'Desktop collapsed state broke mobile drawer');
  await page.getByRole('button', { name: 'Close navigation', exact: true }).click();
  await page.waitForFunction(() => document.querySelector('#sessions-panel').getBoundingClientRect().right <= 0);
  check(await page.locator('body').evaluate(el => el.scrollWidth <= innerWidth), 'Mobile horizontal overflow');
  await page.screenshot({ path: 'tmp/sidebar-check/mobile.png' });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await button('left', true).click();
  await button('right', true).click();
  recorded = false;
  await page.reload();
  await page.getByText('Both sidebars can be collapsed independently.', { exact: true }).waitFor();
  await noCounters();
  await page.addInitScript(() => {
    Storage.prototype.getItem = () => { throw new Error('Storage unavailable'); };
    Storage.prototype.setItem = () => { throw new Error('Storage unavailable'); };
  });
  await page.reload();
  await button('right', false).click();
  await button('left', false).click();
  check(await width('#sessions-panel') === 48 && await width('#prompts-panel') === 48, 'Toggles require localStorage');
  check(errors.length === 0, 'Browser errors: ' + errors.join('; '));
  return { passed: true, checks: ['usage removed', 'Tabler icons and accessible states', 'independent collapse and expansion', 'reload persistence', 'keyboard controls', 'resize restoration', 'library tabs', 'tablet grid', 'mobile navigation', 'missing token counters', 'unavailable storage', 'no browser errors'], screenshots: ['expanded.png', 'collapsed.png', 'mobile.png'] };
}
