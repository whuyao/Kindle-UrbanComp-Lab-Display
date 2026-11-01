// Desktop Chromium simulation; this is NOT Kindle hardware validation.
const assert = require('node:assert/strict');
const path = require('node:path');
require('node:fs').mkdirSync(path.join(__dirname, '../previews'), {recursive: true});
const {chromium} = require(process.env.INK_PLAYWRIGHT_PATH || 'playwright');
(async () => {
  const browser = await chromium.launch({headless:true,
    ...(process.env.INK_CHROME ? {executablePath:process.env.INK_CHROME} : {})});
  try {
    const context = await browser.newContext({viewport:{width:758,height:1024},
      userAgent:'Mozilla/5.0 (Linux) AppleWebKit/534.26 Kindle/3.0+ InkTest'});
    const page = await context.newPage();
    let images=0; const errors=[];
    page.on('request',req=>{if(req.url().includes('/screen.png'))images++;});
    page.on('pageerror',e=>errors.push(e.message));
    await page.goto(process.env.INK_URL || 'http://127.0.0.1:8765/');
    await page.waitForFunction(()=>document.getElementById('page').style.visibility==='visible');
    const first=await page.locator('#page').getAttribute('src');
    assert.equal(images,1);
    await page.evaluate(()=>poll());
    await page.waitForFunction(()=>!busy);
    assert.equal(images,1,'Same version must not re-download or replace image');
    assert.equal(await page.locator('#page').getAttribute('src'),first);
    await page.screenshot({path:path.join(__dirname,'../previews/browser-simulated-758.png')});
    await context.setOffline(true);
    await page.evaluate(()=>poll());
    await page.waitForFunction(()=>document.getElementById('notice').style.display==='block');
    assert.equal(await page.locator('#page').getAttribute('src'),first,'Offline keeps old image');
    await context.setOffline(false);
    await page.evaluate(()=>poll());
    await page.waitForFunction(()=>document.getElementById('notice').style.display==='none');
    await page.locator('#page').click();
    await page.locator('a[href="/reading.html"]').click();
    await page.waitForURL('**/reading.html');
    assert.ok((await page.locator('body').innerText()).includes('Open-Meteo'));
    assert.equal(errors.length,0,errors.join('\n'));
    console.log('PASS: image loads; unchanged version skips refresh; offline retains image; recovery and reading page work.');
  } finally {await browser.close();}
})().catch(error=>{console.error(error);process.exitCode=1;});
