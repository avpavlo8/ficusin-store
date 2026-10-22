import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import vm from 'node:vm';
const listeners=new Map(),shown=[],opened=[],focused=[];
let windows=[];
const self={location:{origin:'https://ficusin.ru'},addEventListener:(name,handler)=>listeners.set(name,handler),registration:{showNotification:async(title,options)=>shown.push({title,options})},clients:{matchAll:async()=>windows,openWindow:async url=>opened.push(url)}};
vm.runInNewContext(await readFile(new URL('../public/sw.js',import.meta.url),'utf8'),{self,URL});
for(const [title,body,url] of [['Фикусин','Здравствуйте! Пришлите фото листьев 🌿','/chat'],['Анна','📷 Фотография','/admin?section=chat&conversation=123']]){
 const tasks=[];listeners.get('push')({data:{json:()=>({title,body,url,tag:'chat-123'})},waitUntil:task=>tasks.push(task)});await Promise.all(tasks);
 const notification=shown.at(-1);assert.equal(notification.title,title);assert.equal(notification.options.body,body);assert.equal(notification.options.data.url,url);
 listeners.get('notificationclick')({notification:{data:notification.options.data,close(){}},waitUntil:task=>tasks.push(task)});await Promise.all(tasks);assert.equal(opened.at(-1),'https://ficusin.ru'+url);
 windows=[{url:'https://ficusin.ru'+url,focus:async()=>focused.push(url)}];
 listeners.get('notificationclick')({notification:{data:notification.options.data,close(){}},waitUntil:task=>tasks.push(task)});await Promise.all(tasks);assert.equal(focused.at(-1),url);windows=[];
}
console.log('Chat service worker: text/attachment previews, closed-site open and existing-dialog focus passed.');
