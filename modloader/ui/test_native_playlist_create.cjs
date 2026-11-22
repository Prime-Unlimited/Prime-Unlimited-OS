const fs=require('fs'),vm=require('vm'),assert=require('assert');
const src=fs.readFileSync(process.argv[2],'utf8');
function extract(name){let a=src.indexOf('function '+name+'('),i=src.indexOf('{',a)+1,d=1;for(;d;i++){if(src[i]==='{')d++;if(src[i]==='}')d--}return src.slice(a,i)}
let calls=0,acks=[],claimStatus=200,device='/media/Denon',fail=false;
const item={visible:true,enabled:true,model:{},rootModelIndex:{row:1},controller:{getSourceDevice(){return device},createList(){calls++;if(fail)throw Error('uncertain')}}};
const command={id:'a'.repeat(64),rootRow:1,sourceDevice:'/media/Denon',title:'one empty test'};
const c={hostItem:{mainScreen:true},createCandidates:[item],handledCommands:{},commandBusy:false,pendingModel:null,pendingCommandID:'',Qt:{callLater:f=>f()},nativeRequest(method,path,data,cb){if(path==='/pending')cb(200,command);else if(path==='/claim')cb(claimStatus,command);else {acks.push(data.stage);cb(200,{})}}};
vm.createContext(c);for(const n of ['acknowledgeCreate','createTargetMatches','pollNativeCreate'])vm.runInContext(extract(n),c);
device='/media/SD Card';c.pollNativeCreate();assert.equal(calls,0);
device='/media/Denon';claimStatus=409;c.pollNativeCreate();assert.equal(calls,0);
c.handledCommands={};claimStatus=200;c.pollNativeCreate();c.pollNativeCreate();assert.equal(calls,1);assert.deepEqual(acks,['submitted']);
c.handledCommands={};acks=[];fail=true;c.pollNativeCreate();c.pollNativeCreate();assert.equal(calls,2);assert.deepEqual(acks,['uncertain']);
console.log('PASS: target mismatch, rejected claim, exactly-once invocation, uncertain exception without retry');

// Add-existing uses the same durable claim and exact target, calling only addTracksToList.
let additions=[];fail=false;item.currentModelIndex={row:35};item.model={data(index,role){return role===900?'2397':'USB Copy Live Test 20261122-124235 - 88487aa6'}};item.controller.addTracksToList=(index,ids)=>additions.push(ids);
c.ListsModel={UniqueIdRole:900};command.operation='add';command.currentRow=35;command.targetListId='2397';command.title='USB Copy Live Test 20261122-124235 - 88487aa6';command.nativeTrackId='2251799813701948';c.handledCommands={};acks=[];c.pollNativeCreate();c.pollNativeCreate();assert.equal(additions.length,1);assert.equal(additions[0][0],2251799813701948);assert.equal(calls,2);
c.handledCommands={};command.targetListId='2396';c.pollNativeCreate();assert.equal(additions.length,1);console.log('PASS: exact native list target, one existing track, no repeated add');
let registrations=[];const collection={collectionModel:{},isInserting:false,isCollectionJobRunning:false,addTracksFromFileSystem(urls,index){registrations.push({urls,index})}};
c.Planck={librarianController:{collectionController:collection,currentCollectionController:collection}};
command.operation='register';command.targetListId='2397';command.fileUrl='file:///media/Denon/Engine%20Library/Music/USB%20Copy%20Native%20Test%20-%20aaaaaaaaaaaaaaaa/sample.mp3';c.handledCommands={};c.pollNativeCreate();c.pollNativeCreate();assert.equal(registrations.length,1);assert.equal(registrations[0].index,-1);assert.equal(additions.length,1);assert.equal(calls,2);
c.handledCommands={};c.Planck.librarianController.currentCollectionController={addTracksFromFileSystem(){throw Error('wrong controller')}};c.pollNativeCreate();assert.equal(registrations.length,1);
c.Planck.librarianController.currentCollectionController=collection;command.fileUrl='file:///media/SD%20Card/sample.mp3';c.pollNativeCreate();assert.equal(registrations.length,1);
console.log('PASS: one native file registration, exact library controller, internal URL only, no duplicate invocation');
