const {chromium}=require('playwright');
const fs=require('node:fs');
const {spawn}=require('node:child_process');
(async()=>{
const file='/tmp/vellum-table-fixture.md';
fs.writeFileSync(file,'# Table journey\n\nIntro\n\n| Heading A | Heading B |\n|---|---|\n'+Array.from({length:80},(_,i)=>`| Row ${i} | Value ${i} |`).join('\n')+'\n\n'+Array(80).fill('After table\n\n').join(''));
const server=spawn(process.env.VELLUM_BINARY || './vellum',['serve-view','--addr','127.0.0.1:19843']);
const browser=await chromium.launch({headless:true,channel:'chrome'});
try {
const page=await browser.newPage();page.on('pageerror',e=>console.error(e));
await page.goto('http://127.0.0.1:19843'+file);
await page.waitForSelector('thead'); await page.waitForFunction(() => document.querySelector('thead').dataset.vellumPinOffset !== undefined);
const measure=()=>page.evaluate(()=>{const t=document.querySelector('article table')||document.querySelector('.vellum-article table');const h=t.tHead.getBoundingClientRect();return {head:h.top,bottom:h.bottom,table:t.getBoundingClientRect().bottom,offset:Number(t.tHead.dataset.vellumPinOffset||0),top:Math.max(document.querySelector('.vellum-scroll').getBoundingClientRect().top,document.querySelector('.vellum-pin-title').getBoundingClientRect().bottom)};});
let m=await measure(); if(m.offset!==0)throw Error('Header moved before scrolling');
await page.locator('.vellum-scroll').evaluate(e=>e.scrollTop=600);await page.waitForTimeout(150);
m=await measure();if(Math.abs(m.head-m.top)>2)throw Error('Not pinned: '+JSON.stringify(m));
await page.locator('.vellum-scroll').evaluate(e=>{e.scrollTop+=document.querySelector('.vellum-article table').getBoundingClientRect().bottom-e.getBoundingClientRect().top-15;});await page.waitForTimeout(150);
m=await measure();if(m.bottom>m.table+2)throw Error('Escaped table: '+JSON.stringify(m));
await page.locator('.vellum-scroll').evaluate(e=>e.scrollTop=0);await page.waitForTimeout(150);
m=await measure();if(m.offset!==0)throw Error('Failed restore');
console.log('PASS: normal flow, pinned heading, table boundary, scroll-back');
}finally{await browser.close();server.kill();fs.unlinkSync(file);}
})().catch(e=>{console.error(e);process.exitCode=1});
