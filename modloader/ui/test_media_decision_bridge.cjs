const fs=require('fs'),vm=require('vm'),assert=require('assert');
const text=fs.readFileSync(process.argv[2],'utf8');
function extract(name){const start=text.indexOf('function '+name+'(');let brace=text.indexOf('{',start),depth=1,end=brace+1;for(;depth;end++){if(text[end]==='{')depth++;if(text[end]==='}')depth--;}return text.slice(start,end);}
const requests=[];function Request(){requests.push(this);this.readyState=0;this.status=0;this.responseText='';this.open=()=>{};this.setRequestHeader=()=>{};this.send=()=>{};this.abort=()=>{this.readyState=4;this.onreadystatechange();};}Request.DONE=4;
const watchdog={starts:0,stops:0,restart(){this.starts++},stop(){this.stops++}};
const ctx={XMLHttpRequest:Request,decisionWatchdog:watchdog,host:{authorized(){}},preparing:false,generation:0,decisionRequest:null,decisionCallback:null,progressRefreshing:false,decisionEventId:"",preparationMessage:""};ctx.mediaImportBridge=ctx;vm.createContext(ctx);vm.runInContext(extract('finishDecision')+'\n'+extract('respond'),ctx);
let results=[];ctx.respond('event','copy','destination',(...args)=>results.push(args));assert(ctx.preparing);const first=requests[0];first.readyState=4;first.status=200;first.responseText='{"prepared":true}';first.onreadystatechange();assert(!ctx.preparing);assert.equal(results.length,1);first.onreadystatechange();assert.equal(results.length,1);
ctx.respond('event','copy','destination',(...args)=>results.push(args));const second=requests[1];ctx.finishDecision(second,0,'');second.abort();assert(!ctx.preparing);assert.equal(results.length,2);assert.equal(results[1][0],false);assert.equal(results[1][1],0);first.onreadystatechange();assert.equal(results.length,2);console.log('PASS: success, watchdog release, duplicate and stale callbacks');

vm.runInContext(extract('refreshPreparation'),ctx);
ctx.respond('event','copy','destination',()=>{});
ctx.refreshPreparation();const progress=requests[requests.length-1];progress.readyState=4;progress.status=200;progress.responseText=JSON.stringify({eventId:'event',stage:'index',done:128,total:16000});progress.onreadystatechange();assert(ctx.preparationMessage.includes('128 / 16000'));
ctx.refreshPreparation();const stale=requests[requests.length-1];ctx.finishDecision(ctx.decisionRequest,200,'{}');const message=ctx.preparationMessage;stale.readyState=4;stale.status=200;stale.responseText=JSON.stringify({eventId:'event',stage:'compare',done:12,total:12});stale.onreadystatechange();assert.equal(ctx.preparationMessage,message);console.log('PASS: live progress and stale progress response');
