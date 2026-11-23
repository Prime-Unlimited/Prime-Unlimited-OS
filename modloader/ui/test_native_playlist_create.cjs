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

vm.runInContext(extract('nativeDeviceTarget'),c);
let selectedDevices=[];const deviceRows=[{deviceRow:1,deviceRoles:{path:'/media/Denon',readOnly:false}}];
c.nativeDeviceRows={count:1,objectAt:i=>deviceRows[i]};c.Planck.librarianController.sourceViewIndex=0;c.Planck.librarianController.craylistController={getSourceDevice:()=>'/media/Denon'};
c.nativeConnectedRows={count:1,objectAt:i=>({sourceRow:0,sourceRoles:{identifier:'/media/Denon'}})};
const browser={connectedSourceModel:()=>({index:(row,column)=>({row,column,kind:'connected-source'})}),select:index=>{assert.equal(index.kind,'connected-source');selectedDevices.push(index)}};c.Planck.librarianController.browserSourceControllerPtr=()=>browser;
c.Planck.librarianController.devicesController={craylistModelRootModelIndex:path=>({row:1,kind:'lists-root'}),getDatabaseUuid:()=> 'library',deviceModel:{index:(row,column)=>({row,column})},select:()=>{throw Error('wrong controller used')}};
command.operation='select';command.identity={libraryUuid:'library'};c.createCandidates=[];c.handledCommands={};c.pollNativeCreate();c.pollNativeCreate();assert.equal(selectedDevices.length,1);assert.equal(selectedDevices[0].row,0);
c.handledCommands={};command.identity.libraryUuid='wrong';c.pollNativeCreate();assert.equal(selectedDevices.length,1);
command.identity.libraryUuid='library';deviceRows.push(deviceRows[0]);c.nativeDeviceRows.count=2;c.pollNativeCreate();assert.equal(selectedDevices.length,1);
console.log('PASS: device selection without visible playlist, exact path/UUID and no duplicate call');
let rootSelections=0;deviceRows.pop();c.nativeDeviceRows.count=1;c.Planck.librarianController.currentCollectionController=collection;c.Planck.librarianController.devicesController.getCurrentDevicePath=()=>'/media/Denon';c.Planck.librarianController.craylistController.select=(index,source,select,update)=>{assert.equal(index.kind,'lists-root');assert.equal(source,1);assert.equal(select,true);assert.equal(update,false);rootSelections++};
command.operation='all_tracks';c.handledCommands={};c.pollNativeCreate();c.pollNativeCreate();assert.equal(rootSelections,1);
c.handledCommands={};c.Planck.librarianController.devicesController.getCurrentDevicePath=()=>'/media/MP3 2';c.pollNativeCreate();assert.equal(rootSelections,1);
console.log('PASS: internal All Tracks selection only after verified destination binding, without visible playlist');
for(const name of ['lookupContextMatches','beginNativeLookup','scanNativeLookupChunk'])vm.runInContext(extract(name),c);
let lookupReads=0,uriReads=0,lookupResults=[];const previousRequest=c.nativeRequest;c.nativeRequest=(method,path,data,cb)=>{if(data&&data.stage==='lookup_result')lookupResults.push(data);previousRequest(method,path,data,cb)};
c.Planck.librarianController.devicesController.getCurrentDevicePath=()=>'/media/Denon';collection.getDevicePath=()=>'/media/Denon';collection.getCraylistIdOfCurrentCollectionDisplayed=()=>-1;collection.getTrackUrisFromUniversalIds=ids=>{uriReads++;assert.equal(ids[0],2251799813701950);return '["file:///media/Denon/Engine%20Library/Music/new.mp3"]'};
collection.collectionModel={rowCount:()=>1001,index:row=>({row}),data:index=>{lookupReads++;return index.row===999?2251799813701950:2251799813685249+index.row}};c.AbstractCollectionModel={TrackId:271};command.operation='lookup';command.lookupLocalId=16702;c.lookupState=null;c.beginNativeLookup(command);c.scanNativeLookupChunk();assert(lookupReads<=200);assert.equal(uriReads,0);let chunks=0;while(c.lookupState&&chunks++<100)c.scanNativeLookupChunk();assert.equal(lookupResults.length,1);assert.equal(lookupResults[0].nativeTrackId,'2251799813701950');assert.equal(uriReads,1);assert.equal(registrations.length,1);
c.beginNativeLookup(command);collection.getDevicePath=()=>'/media/MP3 2';c.scanNativeLookupChunk();assert.equal(c.lookupState,null);assert.equal(uriReads,1);assert.equal(acks[acks.length-1],'lookup_failed');
console.log('PASS: bounded native ID scan, one real URI lookup, no selection/import writes, context-change abort');
