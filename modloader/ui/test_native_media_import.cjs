const fs = require('fs');
const vm = require('vm');
const assert = require('assert');
const source = fs.readFileSync(process.argv[2], 'utf8');
function extract(name) {
    let start = source.indexOf('    function ' + name + '(');
    assert(start >= 0, name);
    let cursor = source.indexOf('{', start) + 1, depth = 1;
    while (depth) { if (source[cursor] === '{') depth++; if (source[cursor] === '}') depth--; cursor++; }
    return source.slice(start, cursor);
}
const names = [...source.matchAll(/^    function ([A-Za-z0-9_]+)\(/gm)].map(m => m[1]);
function fixture(operation) {
    const job = 'a'.repeat(64);
    const binding = {LibraryUUID:'647495f9-fd0e-4772-95b9-c69de5aac767', MountIdentity:'b'.repeat(64), EngineIdentity:'boot:970:1516'};
    const command = {jobId:job, id:'c'.repeat(64), operation, binding, expiresAt:new Date(Date.now()+60000).toISOString()};
    const envelope = {command, sourceDevice:'/media/Denon', expiresUnixMs:Date.now()+60000};
    const state = {path:'/media/Denon', uuid:binding.LibraryUUID, spinner:false, busy:false, listID:-1, nativeCalls:[], requests:[], observations:[], claimStatus:200, rows:[201,202,203], titles:{}, autoCreate:true, autoAdd:true, uriReads:0};
    const c = {hostItem:{mainScreen:true}, jobs:[{id:job,mode:'live',phase:'copying'}], requesting:false, pending:null,lastHandledID:'', signalModel:null,signalController:null,registrationController:null,
        ListsModel:{UniqueIdRole:900}, AbstractCollectionModel:{TrackId:271}};
    const model = {fetchingContent:false,rowCount:()=>state.rows.length,index:(row,column,parent)=>({row,column,kind:'child',model}),data:(index,role)=>index.kind==='root'?0:role===0?(state.titles[state.rows[index.row]]||'Target List '+state.rows[index.row]):state.rows[index.row]};
    const root = {row:1,column:0,kind:'root',model};
    const controller = {
        getSourceDevice:()=>'/media/Denon',
        select(index,view,select,update){assert.strictEqual(index,root);assert.equal(view,1);assert.equal(select,true);assert.equal(update,false);state.nativeCalls.push('all_tracks');state.listID=-1},
        createList(index,title){assert.strictEqual(index,root);assert(title);state.nativeCalls.push('create');if(state.autoCreate){state.rows.push(204);state.titles[204]=title;c.inserted(root,state.rows.length-1,state.rows.length-1)}},
        addTracksToList(index,ids){assert.equal(model.data(index),202);state.nativeCalls.push({action:'add',ids:[...ids]});if(state.autoAdd)c.tracksAdded(index)}
    };
    const collection = {getDevicePath:()=>state.path,getCraylistIdOfCurrentCollectionDisplayed:()=>state.listID,
        get isCollectionJobRunning(){return state.busy},isInserting:false,
        addTracksFromFileSystem(urls,index){assert.equal(index,-1);state.nativeCalls.push({action:'register',urls:[...urls]});state.busy=true;c.collectionComplete()},
        getTrackUrisFromUniversalIds(ids){state.uriReads++;return 'file:///media/Denon/Engine%20Library/Music/new-'+ids[0]+'.mp3'},
        collectionModel:{rowCount:()=>1001,index:row=>({row}),data:index=>index.row===999?2251799813701950:index.row===500?2251799813701948:2251799813685249+index.row}
    };
    const browser={connectedSourceModel:()=>({index:(row,column)=>({row,column})}),select(index){assert.equal(index.row,0);state.nativeCalls.push('select');if(state.onSelect)state.onSelect();else state.path='/media/Denon'}};
    const devices={getDatabaseUuid:()=>state.uuid,getCurrentDevicePath:()=>state.path,craylistModelRootModelIndex:()=>root};
    c.Planck={librarianController:{devicesController:devices,browserSourceControllerPtr:()=>browser,currentCollectionController:collection,collectionController:collection,craylistController:controller}};
    c.deviceRows={count:1,objectAt:()=>({roles:{path:'/media/Denon',readOnly:false}})};
    c.sourceRows={count:1,objectAt:()=>({sourceRow:0,roles:{identifier:'/media/Denon',hasSpinner:state.spinner}})};
    vm.createContext(c);
    for(const name of names)vm.runInContext(extract(name),c);
    let now=Date.now();c.Date={now:()=>now};const advance=c.advance;c.advance=()=>{now+=16;advance()};
    c.nativeRequest=(jobID,action,body,done)=>{
        assert.equal(jobID,job);state.requests.push(action);
        if(action==='pending')done(200,envelope);
        else if(action==='claim'){if(state.onClaim)state.onClaim();done(state.claimStatus,state.claimStatus===200?envelope:null)}
        else{assert.equal(action,'observe');state.observations.push(JSON.parse(JSON.stringify(body.observation)));done(202,{recorded:true})}
    };
    function finish(){let ticks=0;while(c.pending&&ticks++<200)c.advance();assert.equal(c.pending,null,'command did not finish')}
    return {c,state,command,envelope,binding,root,model,controller,collection,finish};
}
{
    const f=fixture('select_library');let reads=0;
    function Request(){this.open=(method,url)=>{assert.equal(method,'GET');assert.equal(url,'http://127.0.0.1:8098/api/v1/media-import/jobs')};this.setRequestHeader=(key,value)=>{assert.equal(key,'Authorization');assert.equal(value,'Bearer fixture-token')};this.send=()=>{reads++;this.status=200;this.readyState=4;this.responseText=JSON.stringify({jobs:[{id:'a'.repeat(64),mode:'live',phase:'copying'}]});this.onreadystatechange()}}
    Request.DONE=4;f.c.XMLHttpRequest=Request;f.c.authToken='fixture-token';f.c.jobs=[];f.c.jobsRefreshing=false;
    f.c.refreshJobs();assert.equal(reads,1);assert.equal(f.c.activeJob(),'a'.repeat(64));f.c.hostItem=null;f.c.refreshJobs();assert.equal(reads,1);
    console.log('PASS: standalone trusted actor polls authorized jobs only on main screen');
}

{
    const f=fixture('select_library');f.state.path='/media/MP3 2';
    f.state.onSelect=()=>{f.state.spinner=true};
    f.c.poll();assert.deepEqual(f.state.nativeCalls,['select']);f.c.advance();assert(f.c.pending,'normal loading was rejected');
    f.state.spinner=false;f.state.path='/media/Denon';f.finish();f.c.poll();
    assert.deepEqual(f.state.nativeCalls,['select']);assert.equal(f.state.observations[0].outcome,'applied');
    console.log('PASS: asynchronous native source selection, loading tolerated, one invocation');
}
{
    for(const claimStatus of [409,0]){
        const f=fixture('select_library');f.state.claimStatus=claimStatus;f.c.poll();f.c.poll();
        assert.equal(f.state.nativeCalls.length,0);assert.equal(f.state.requests.filter(r=>r==='claim').length,1);
    }
    const f=fixture('select_library');f.state.onClaim=()=>{f.state.uuid='different'};f.c.poll();
    assert.equal(f.state.nativeCalls.length,0);assert.equal(f.state.observations[0].outcome,'not_invoked');
    console.log('PASS: rejected/ambiguous claim and changed native UUID never invoke');
}
{
    const f=fixture('all_tracks');f.state.listID=2396;f.c.poll();f.finish();
    assert.deepEqual(f.state.nativeCalls,['all_tracks']);assert.equal(f.state.observations[0].outcome,'applied');
    const blocked=fixture('all_tracks');blocked.state.path='/media/MP3 2';blocked.c.poll();
    assert.equal(blocked.state.nativeCalls.length,0);assert.equal(blocked.state.observations[0].outcome,'not_invoked');
    console.log('PASS: All Tracks selection requires the actual current internal collection');
}
{
    const f=fixture('register_files');f.command.paths=['Music/Imported/new song.mp3'];f.envelope.fileUrls=['file:///media/Denon/Engine%20Library/Music/Imported/new%20song.mp3'];
    f.c.poll();assert(f.c.pending);f.c.advance();assert.equal(f.state.observations.length,0,'completion accepted while native collection busy');
    f.state.busy=false;f.finish();f.c.collectionComplete();f.c.poll();
    assert.equal(f.state.nativeCalls.length,1);assert.equal(f.state.nativeCalls[0].action,'register');assert.equal(f.state.observations.length,1);
    const bad=fixture('register_files');bad.command.paths=['Music/Imported/new.mp3'];bad.envelope.fileUrls=['file:///media/SD%20Card/new.mp3'];bad.c.poll();
    assert.equal(bad.state.nativeCalls.length,0);assert.equal(bad.state.observations[0].outcome,'not_invoked');
    console.log('PASS: internal-file registration waits for native completion and idle, no SD URI');
}
{
    const f=fixture('lookup_tracks');f.command.localIds=[16702,16700];f.c.poll();f.c.advance();
    assert(f.c.pending.cursor<=200,'unbounded GUI scan');f.finish();
    const result=f.state.observations[0];assert.equal(result.outcome,'applied');
    assert.deepEqual(result.tracks.map(t=>t.NativeID),['2251799813701950','2251799813701948']);
    assert.equal(result.uris.length,2);assert.equal(f.state.uriReads,2);assert.equal(f.state.nativeCalls.length,0);
    const changed=fixture('lookup_tracks');changed.command.localIds=[16702];changed.c.poll();changed.state.path='/media/MP3 2';changed.finish();
    assert.equal(changed.state.observations[0].outcome,'not_invoked');assert.equal(changed.state.uriReads,0);
    console.log('PASS: bounded batch lookup reads actual model IDs and URIs in requested order, aborts source change');
}
{
    const f=fixture('create_playlist');f.command.title='Only newly copied songs';f.c.poll();f.finish();
    assert.deepEqual(f.state.nativeCalls,['create']);assert.equal(f.state.observations[0].playlist.NativeID,'204');
    assert(!f.state.observations[0].playlist.LocalID,'actor guessed SQL ID from native ID');
    f.c.inserted(f.root,3,3);f.c.poll();assert.equal(f.state.observations.length,1);assert.equal(f.state.nativeCalls.length,1);
    const foreign=fixture('create_playlist');foreign.state.autoCreate=false;foreign.command.title='new';foreign.c.poll();foreign.c.advance();
    foreign.c.inserted(foreign.model.index(1,0,foreign.root),0,0);assert.equal(foreign.c.pending.inserted.length,0,'foreign-folder insertion accepted');
    foreign.envelope.expiresUnixMs=0;foreign.finish();assert.equal(foreign.state.observations[0].outcome,'uncertain');
    console.log('PASS: playlist creation reports observed native ID, no SQL ID guess, ignores unrelated folder signal');
}
function prepareAdd(f){
    f.command.playlist={NativeID:'202',LocalID:201,Binding:f.binding,Title:'Target List 202'};
    f.command.tracks=[{NativeID:'2251799813701950',LocalID:16702,Binding:f.binding,SourceRelative:'new.mp3'}];
    f.command.trackUris=['file:///media/Denon/Engine%20Library/Music/new-2251799813701950.mp3'];
}
{
    const f=fixture('create_playlist');f.command.title='Asynchronous creation';
    const create=f.controller.createList;
    f.controller.createList=(index,title)=>{create(index,title);f.model.fetchingContent=true;f.state.rows[f.state.rows.length-1]=0;f.c.inserted(f.root,3,3)};
    f.c.poll();f.c.advance();f.c.advance();assert(f.c.pending);assert.equal(f.state.observations.length,0);
    f.model.fetchingContent=false;f.state.rows[3]=205;f.state.titles[205]=f.command.title;f.c.inserted(f.root,3,3);f.finish();
    assert.deepEqual(f.state.nativeCalls,['create']);assert.equal(f.state.observations[0].playlist.NativeID,'205');
    console.log('PASS: provisional IDs, repeated insert signals and asynchronous model loading settle without repeat creation');
}
{
    const f=fixture('add_tracks');prepareAdd(f);f.c.poll();f.finish();
    assert.equal(f.state.nativeCalls.length,1);assert.deepEqual(f.state.nativeCalls[0],{action:'add',ids:[2251799813701950]});
    assert.equal(f.state.observations[0].outcome,'applied');f.c.poll();assert.equal(f.state.nativeCalls.length,1);
    const stale=fixture('add_tracks');prepareAdd(stale);stale.command.trackUris=['file:///other.mp3'];stale.c.poll();stale.finish();
    assert.equal(stale.state.nativeCalls.length,0);assert.equal(stale.state.observations[0].outcome,'not_invoked');
    const wrong=fixture('add_tracks');prepareAdd(wrong);wrong.command.playlist.NativeID='999';wrong.c.poll();wrong.finish();assert.equal(wrong.state.nativeCalls.length,0);
    console.log('PASS: exact native playlist and current URI proofs required, one add invocation');
}
{
    const f=fixture('select_library');f.state.path='/media/MP3 2';f.c.poll();f.c.hostItem.mainScreen=false;f.finish();
    assert.equal(f.state.observations[0].outcome,'uncertain');assert.equal(f.state.nativeCalls.length,1);
    for(const type of ['arbitrary_sql','expired','ambiguous_parent']){
        const b=fixture(type==='arbitrary_sql'?type:'select_library');
        if(type==='expired')b.envelope.expiresUnixMs=0;
        if(type==='ambiguous_parent')b.c.jobs.push({...b.c.jobs[0],id:'d'.repeat(64)});
        b.c.poll();assert.equal(b.state.nativeCalls.length,0);assert(!b.state.requests.includes('claim'));
    }
    console.log('PASS: screen handoff, expired command, unknown operation and ambiguous parent fail closed');
}
{
    for(const operation of ['select_library','all_tracks']){
        const f=fixture(operation);f.c.poll();f.finish();assert.equal(f.state.nativeCalls.length,0);assert.equal(f.state.observations[0].outcome,'applied');
    }
    console.log('PASS: already selected internal All Tracks view is preserved without native reselection');
}
